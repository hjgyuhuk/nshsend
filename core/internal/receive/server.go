package receive

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"nshsend/core/internal/identity"
	"nshsend/core/internal/shared"
)

// IncomingOfferEvent is emitted to the UI when a sender proposes a transfer.
type IncomingOfferEvent struct {
	SessionID         string                        `json:"sessionId"`
	SenderAlias       string                        `json:"senderAlias"`
	SenderIP          string                        `json:"senderIp"`
	SenderDeviceModel string                        `json:"senderDeviceModel,omitempty"`
	SenderDeviceType  shared.DeviceType             `json:"senderDeviceType,omitempty"`
	SenderFingerprint string                        `json:"senderFingerprint"`
	SenderVerified    bool                          `json:"senderVerified"`
	Files             map[string]shared.FileOfferV2 `json:"files"`
}

// ProgressEvent reports per-file receive progress.
type ProgressEvent struct {
	SessionID        string `json:"sessionId"`
	FileID           string `json:"fileId"`
	FileName         string `json:"fileName"`
	BytesTransferred uint64 `json:"bytesTransferred"`
	TotalBytes       uint64 `json:"totalBytes"`
	CompletedFiles   int    `json:"completedFiles"`
	TotalFiles       int    `json:"totalFiles"`
}

// SessionEndEvent reports completion or cancellation of an incoming transfer.
type SessionEndEvent struct {
	SessionID string   `json:"sessionId"`
	Reason    string   `json:"reason"` // "finished" | "cancelled" | "failed"
	SavedDir  string   `json:"savedDir,omitempty"`
	Files     []string `json:"files,omitempty"`
	TextBody  string   `json:"textBody,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// ServerCallbacks decouples receive HTTP handling from IPC and discovery.
type ServerCallbacks struct {
	OnRegister       func(addr shared.PeerAddr, info shared.RegisterRequestV2)
	OnIncomingOffer  func(offer IncomingOfferEvent)
	OnOfferAborted   func(sessionID string)
	OnProgress       func(progress ProgressEvent)
	OnSessionEnd     func(end SessionEndEvent)
	OnRemoteCancel   func(peerIP string, sessionID string)
	OnHistoryUpdated func(entries []HistoryEntry)
}

// ReceiveServer implements the LocalSend v2 HTTP/HTTPS receiver endpoints.
type ReceiveServer struct {
	cert      *identity.DeviceCertificate
	history   *HistoryStore
	callbacks ServerCallbacks
	pins      *PinAttemptTracker

	mu        sync.Mutex
	settings  identity.AppSettings
	slot      sessionSlot
	listeners []net.Listener
	httpSrv   *http.Server
	boundPort uint16
}

// NewReceiveServer constructs a ReceiveServer.
func NewReceiveServer(
	cert *identity.DeviceCertificate,
	settings identity.AppSettings,
	history *HistoryStore,
	callbacks ServerCallbacks,
) *ReceiveServer {
	return &ReceiveServer{
		cert:      cert,
		settings:  settings,
		history:   history,
		callbacks: callbacks,
		pins:      NewPinAttemptTracker(),
	}
}

// StartServer binds IPv4 (and best-effort IPv6) listeners and begins serving v2 endpoints.
func (s *ReceiveServer) StartServer(settings identity.AppSettings) (uint16, error) {
	s.StopServer()

	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()

	addrV4 := fmt.Sprintf("0.0.0.0:%d", settings.Port)
	lnV4, err := net.Listen("tcp4", addrV4)
	if err != nil {
		return 0, fmt.Errorf("listen %s: %w", addrV4, err)
	}
	boundPort := uint16(lnV4.Addr().(*net.TCPAddr).Port)

	listeners := []net.Listener{lnV4}
	addrV6 := fmt.Sprintf("[::]:%d", boundPort)
	if lnV6, errV6 := net.Listen("tcp6", addrV6); errV6 == nil {
		listeners = append(listeners, lnV6)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/info", s.handleInfo)
	mux.HandleFunc("/api/localsend/v2/register", s.handleRegister)
	mux.HandleFunc("/api/localsend/v2/prepare-upload", s.handlePrepareUpload)
	mux.HandleFunc("/api/localsend/v2/upload", s.handleUpload)
	mux.HandleFunc("/api/localsend/v2/cancel", s.handleCancel)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	if settings.HTTPS {
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{s.cert.TLSCert},
			ClientAuth:   tls.RequestClientCert,
			MinVersion:   tls.VersionTLS12,
		}
	}

	s.mu.Lock()
	s.listeners = listeners
	s.httpSrv = srv
	s.boundPort = boundPort
	s.mu.Unlock()

	for _, ln := range listeners {
		listener := ln
		go func() {
			if settings.HTTPS {
				tlsLn := tls.NewListener(listener, srv.TLSConfig)
				_ = srv.Serve(tlsLn)
			} else {
				_ = srv.Serve(listener)
			}
		}()
	}

	return boundPort, nil
}

// StopServer shuts down the HTTP/HTTPS listeners and clears any pending session.
func (s *ReceiveServer) StopServer() {
	s.mu.Lock()
	srv := s.httpSrv
	listeners := s.listeners
	s.httpSrv = nil
	s.listeners = nil
	s.boundPort = 0
	if s.slot.pending != nil {
		close(s.slot.pending.cancelCh)
		s.slot.pending = nil
	}
	s.slot.active = nil
	s.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
}

// BoundPort returns the port the server is currently listening on.
func (s *ReceiveServer) BoundPort() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.boundPort
}

// RespondToIncomingOffer delivers the user's accept/decline decision for a pending session.
func (s *ReceiveServer) RespondToIncomingOffer(sessionID string, accept bool, acceptedFileIDs []string) error {
	s.mu.Lock()
	pending := s.slot.pending
	if pending == nil || pending.sessionID != sessionID {
		s.mu.Unlock()
		return fmt.Errorf("no pending offer with sessionId %q", sessionID)
	}
	decisionCh := pending.decisionCh
	s.mu.Unlock()

	decision := PendingOfferDecision{Declined: !accept}
	if accept {
		decision.AcceptedIDs = make(map[string]struct{}, len(acceptedFileIDs))
		for _, id := range acceptedFileIDs {
			decision.AcceptedIDs[id] = struct{}{}
		}
	}

	select {
	case decisionCh <- decision:
		return nil
	default:
		return fmt.Errorf("offer decision already submitted")
	}
}

// CancelIncomingSession aborts an active or pending receive session from the receiver UI.
func (s *ReceiveServer) CancelIncomingSession(sessionID string) bool {
	s.mu.Lock()
	if s.slot.pending != nil && s.slot.pending.sessionID == sessionID {
		select {
		case s.slot.pending.decisionCh <- PendingOfferDecision{Declined: true}:
		default:
		}
		s.mu.Unlock()
		return true
	}
	if s.slot.active != nil && s.slot.active.sessionID == sessionID {
		s.slot.active = nil
		s.mu.Unlock()
		if s.callbacks.OnSessionEnd != nil {
			s.callbacks.OnSessionEnd(SessionEndEvent{
				SessionID: sessionID,
				Reason:    "cancelled",
			})
		}
		return true
	}
	s.mu.Unlock()
	return false
}

func (s *ReceiveServer) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	s.mu.Lock()
	cfg := s.settings
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, shared.InfoResponseV2{
		Alias:       cfg.Alias,
		Version:     shared.ProtocolVersionV2,
		DeviceModel: cfg.DeviceModel,
		DeviceType:  shared.DeviceDesktop,
		Fingerprint: s.cert.Fingerprint,
		Download:    false,
	})
}

func (s *ReceiveServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var payload shared.RegisterRequestV2
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	senderFP, verified := resolveSenderFingerprint(r, payload.Fingerprint)
	fingerprintValid := senderFP != "" && strings.EqualFold(senderFP, payload.Fingerprint)
	if r.TLS != nil && !verified {
		fingerprintValid = false
	}

	if fingerprintValid && s.callbacks.OnRegister != nil {
		if peerAddr, err := parseRemotePeerAddr(r.RemoteAddr); err == nil {
			log.Printf("[receive] register from %s (%s, tlsVerified=%v)", peerAddr.IP, payload.Alias, verified)
			s.callbacks.OnRegister(peerAddr, payload)
		}
	}

	s.mu.Lock()
	cfg := s.settings
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, shared.RegisterResponseV2{
		Alias:       cfg.Alias,
		Version:     shared.ProtocolVersionV2,
		DeviceModel: cfg.DeviceModel,
		DeviceType:  shared.DeviceDesktop,
		Fingerprint: s.cert.Fingerprint,
		Download:    false,
	})
}

func (s *ReceiveServer) handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	peerAddr, err := parseRemotePeerAddr(r.RemoteAddr)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "Invalid remote address")
		return
	}
	remoteIP := peerAddr.IP.String()

	s.mu.Lock()
	cfg := s.settings
	s.mu.Unlock()

	query := r.URL.Query()
	hasPin := query.Has("pin")
	if status, msg := s.pins.ValidatePin(cfg.PIN, query.Get("pin"), hasPin, remoteIP); status != 0 {
		writeErrorJSON(w, status, msg)
		return
	}

	var payload shared.PrepareUploadRequestV2
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&payload); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if len(payload.Files) == 0 {
		writeErrorJSON(w, http.StatusBadRequest, "No files provided")
		return
	}

	senderFP, verified := resolveSenderFingerprint(r, payload.Info.Fingerprint)
	if r.TLS != nil && verified && !strings.EqualFold(senderFP, payload.Info.Fingerprint) {
		writeErrorJSON(w, http.StatusForbidden, "Fingerprint does not match the client certificate")
		return
	}

	sessionID, err := shared.NewRandomUUIDv4()
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "Failed to allocate session ID")
		return
	}

	pending := &pendingSession{
		sessionID:  sessionID,
		senderIP:   remoteIP,
		decisionCh: make(chan PendingOfferDecision, 1),
		cancelCh:   make(chan struct{}),
	}

	s.mu.Lock()
	if s.slot.isBusy() {
		s.mu.Unlock()
		log.Printf("[receive] prepare-upload from %s rejected: busy", remoteIP)
		writeErrorJSON(w, http.StatusConflict, "Blocked by another session")
		return
	}
	s.slot.pending = pending
	s.mu.Unlock()
	log.Printf("[receive] prepare-upload session=%s from %s (%s, %d items, tlsVerified=%v)", sessionID, remoteIP, payload.Info.Alias, len(payload.Files), verified)

	disarmed := false
	defer func() {
		if !disarmed {
			s.mu.Lock()
			if s.slot.pending == pending {
				s.slot.pending = nil
			}
			s.mu.Unlock()
			if s.callbacks.OnOfferAborted != nil {
				s.callbacks.OnOfferAborted(sessionID)
			}
		}
	}()

	var decision PendingOfferDecision
	if cfg.QuickSave == identity.QuickSaveOn {
		accepted := make(map[string]struct{}, len(payload.Files))
		for id := range payload.Files {
			accepted[id] = struct{}{}
		}
		decision = PendingOfferDecision{AcceptedIDs: accepted}
	} else {
		if s.callbacks.OnIncomingOffer == nil {
			writeErrorJSON(w, http.StatusForbidden, "Rejected")
			return
		}
		s.callbacks.OnIncomingOffer(IncomingOfferEvent{
			SessionID:         sessionID,
			SenderAlias:       payload.Info.Alias,
			SenderIP:          remoteIP,
			SenderDeviceModel: payload.Info.DeviceModel,
			SenderDeviceType:  payload.Info.DeviceType,
			SenderFingerprint: senderFP,
			SenderVerified:    verified,
			Files:             payload.Files,
		})

		select {
		case decision = <-pending.decisionCh:
		case <-pending.cancelCh:
			writeErrorJSON(w, http.StatusForbidden, "Cancelled by sender")
			return
		case <-r.Context().Done():
			return
		}
	}

	if decision.Declined {
		writeErrorJSON(w, http.StatusForbidden, "Rejected")
		return
	}

	acceptedText := make([]shared.FileOfferV2, 0)
	sessionFiles := make(map[string]*SessionFile)
	tokens := make(map[string]string)

	for id, offer := range payload.Files {
		if len(decision.AcceptedIDs) > 0 {
			if _, ok := decision.AcceptedIDs[id]; !ok {
				continue
			}
		}
		if shared.IsTextOnlyOffer(offer) {
			acceptedText = append(acceptedText, offer)
			continue
		}
		tok, tokErr := shared.NewRandomUUIDv4()
		if tokErr != nil {
			writeErrorJSON(w, http.StatusInternalServerError, "Failed to generate file token")
			return
		}
		sessionFiles[id] = &SessionFile{
			Offer:  offer,
			Token:  tok,
			Status: FileStatusPending,
		}
		tokens[id] = tok
	}

	var textParts []string
	for _, txtOffer := range acceptedText {
		textParts = append(textParts, txtOffer.Preview)
		s.recordHistory(HistoryEntry{
			ID:          sessionID + ":" + txtOffer.ID,
			FileName:    txtOffer.FileName,
			FileType:    txtOffer.FileType,
			Size:        txtOffer.Size,
			TextPreview: txtOffer.Preview,
			SenderAlias: payload.Info.Alias,
			TimestampMs: time.Now().UnixMilli(),
			IsText:      true,
		})
	}

	// Text-only fast path (HTTP 204, no upload requests follow).
	if len(sessionFiles) == 0 {
		disarmed = true
		s.mu.Lock()
		if s.slot.pending == pending {
			s.slot.pending = nil
		}
		s.mu.Unlock()

		if len(acceptedText) == 0 {
			writeErrorJSON(w, http.StatusForbidden, "No files accepted")
			return
		}
		if s.callbacks.OnSessionEnd != nil {
			s.callbacks.OnSessionEnd(SessionEndEvent{
				SessionID: sessionID,
				Reason:    "finished",
				TextBody:  strings.Join(textParts, "\n"),
			})
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.mu.Lock()
	if s.slot.pending == pending {
		s.slot.pending = nil
	}
	s.slot.active = &activeSession{
		sessionID:   sessionID,
		senderIP:    remoteIP,
		senderAlias: payload.Info.Alias,
		files:       sessionFiles,
	}
	s.mu.Unlock()
	disarmed = true

	writeJSON(w, http.StatusOK, shared.PrepareUploadResponseV2{
		SessionID: sessionID,
		Files:     tokens,
	})
}

func (s *ReceiveServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	peerAddr, err := parseRemotePeerAddr(r.RemoteAddr)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "Invalid remote address")
		return
	}
	remoteIP := peerAddr.IP.String()

	q := r.URL.Query()
	sessionID := q.Get("sessionId")
	fileID := q.Get("fileId")
	token := q.Get("token")
	if sessionID == "" || fileID == "" || token == "" {
		writeErrorJSON(w, http.StatusBadRequest, "Missing parameters")
		return
	}

	s.mu.Lock()
	active := s.slot.active
	if active == nil || active.sessionID != sessionID || active.senderIP != remoteIP {
		s.mu.Unlock()
		writeErrorJSON(w, http.StatusForbidden, "Invalid token or IP address")
		return
	}
	sf, ok := active.files[fileID]
	if !ok || sf.Token != token || sf.Status != FileStatusPending {
		s.mu.Unlock()
		writeErrorJSON(w, http.StatusForbidden, "Invalid token or IP address")
		return
	}
	sf.Status = FileStatusInProgress
	totalFiles := len(active.files)
	senderAlias := active.senderAlias
	cfg := s.settings
	s.mu.Unlock()

	savedPath, _, saveErr := SaveUploadStream(
		r.Context(),
		r.Body,
		cfg.SaveDir,
		sf.Offer,
		cfg.VerifyChecksums,
		func(transferred uint64) {
			if s.callbacks.OnProgress != nil {
				s.callbacks.OnProgress(ProgressEvent{
					SessionID:        sessionID,
					FileID:           fileID,
					FileName:         sf.Offer.FileName,
					BytesTransferred: transferred,
					TotalBytes:       sf.Offer.Size,
					CompletedFiles:   s.countCompletedFiles(sessionID),
					TotalFiles:       totalFiles,
				})
			}
		},
	)

	s.mu.Lock()
	allDone := false
	var savedFiles []string
	if s.slot.active != nil && s.slot.active.sessionID == sessionID {
		if saveErr == nil {
			sf.Status = FileStatusFinished
		} else {
			sf.Status = FileStatusFailed
		}
		allDone = true
		for _, f := range s.slot.active.files {
			if f.Status == FileStatusPending || f.Status == FileStatusInProgress {
				allDone = false
			}
			if f.Status == FileStatusFinished {
				savedFiles = append(savedFiles, f.Offer.FileName)
			}
		}
		if allDone {
			s.slot.active = nil
		}
	}
	s.mu.Unlock()

	if saveErr == nil {
		s.recordHistory(HistoryEntry{
			ID:          sessionID + ":" + fileID,
			FileName:    sf.Offer.FileName,
			FileType:    sf.Offer.FileType,
			Size:        sf.Offer.Size,
			SavedPath:   savedPath,
			SenderAlias: senderAlias,
			TimestampMs: time.Now().UnixMilli(),
			IsText:      false,
		})
	}

	if allDone && s.callbacks.OnSessionEnd != nil {
		reason := "finished"
		errMsg := ""
		if saveErr != nil && len(savedFiles) == 0 {
			reason = "failed"
			errMsg = saveErr.Error()
		}
		s.callbacks.OnSessionEnd(SessionEndEvent{
			SessionID: sessionID,
			Reason:    reason,
			SavedDir:  cfg.SaveDir,
			Files:     savedFiles,
			Error:     errMsg,
		})
	}

	if saveErr != nil {
		log.Printf("[receive] upload failed session=%s file=%s: %v", sessionID, sf.Offer.FileName, saveErr)
		if errors.Is(saveErr, ErrChecksumMismatch) {
			writeErrorJSON(w, http.StatusUnprocessableEntity, "Checksum mismatch")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, saveErr.Error())
		return
	}
	log.Printf("[receive] upload saved session=%s path=%s", sessionID, savedPath)
	w.WriteHeader(http.StatusOK)
}

func (s *ReceiveServer) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	peerAddr, err := parseRemotePeerAddr(r.RemoteAddr)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	remoteIP := peerAddr.IP.String()
	sessionID := r.URL.Query().Get("sessionId")

	s.mu.Lock()
	if s.slot.pending != nil && s.slot.pending.senderIP == remoteIP && (sessionID == "" || s.slot.pending.sessionID == sessionID) {
		close(s.slot.pending.cancelCh)
		s.slot.pending = nil
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	if s.slot.active != nil && s.slot.active.senderIP == remoteIP && s.slot.active.sessionID == sessionID {
		s.slot.active = nil
		s.mu.Unlock()
		if s.callbacks.OnSessionEnd != nil {
			s.callbacks.OnSessionEnd(SessionEndEvent{
				SessionID: sessionID,
				Reason:    "cancelled",
			})
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	s.mu.Unlock()

	if sessionID != "" && s.callbacks.OnRemoteCancel != nil {
		s.callbacks.OnRemoteCancel(remoteIP, sessionID)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *ReceiveServer) countCompletedFiles(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slot.active == nil || s.slot.active.sessionID != sessionID {
		return 0
	}
	count := 0
	for _, f := range s.slot.active.files {
		if f.Status == FileStatusFinished {
			count++
		}
	}
	return count
}

func (s *ReceiveServer) recordHistory(entry HistoryEntry) {
	if s.history == nil {
		return
	}
	if err := s.history.AppendHistoryEntry(entry); err == nil && s.callbacks.OnHistoryUpdated != nil {
		s.callbacks.OnHistoryUpdated(s.history.ListHistoryEntries())
	}
}

func resolveSenderFingerprint(r *http.Request, claimed string) (string, bool) {
	if r.TLS == nil {
		return claimed, false
	}
	if len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	fp := identity.ComputeCertFingerprintDER(r.TLS.PeerCertificates[0].Raw)
	return fp, true
}

func parseRemotePeerAddr(remoteAddr string) (shared.PeerAddr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return shared.PeerAddr{}, err
	}
	return shared.PeerAddr{IP: ip.Unmap()}, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}
