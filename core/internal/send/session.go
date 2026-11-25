package send

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"nshsend/core/internal/identity"
	"nshsend/core/internal/shared"
)

// StagedItemKind distinguishes local files from inline text messages.
type StagedItemKind string

const (
	StagedFile StagedItemKind = "file"
	StagedText StagedItemKind = "text"
)

// StagedItemInput is one item queued by the UI for sending.
type StagedItemInput struct {
	Kind StagedItemKind `json:"kind"`
	Path string         `json:"path,omitempty"`
	Text string         `json:"text,omitempty"`
	Name string         `json:"name,omitempty"`
}

// TargetPeerEndpoint identifies the destination peer for an outbound transfer.
type TargetPeerEndpoint struct {
	Fingerprint string
	Host        string
	Port        uint16
	Protocol    shared.ProtocolType
}

// TransferProgressEvent reports outbound upload progress.
type TransferProgressEvent struct {
	TransferID       string `json:"transferId"`
	FileID           string `json:"fileId"`
	FileName         string `json:"fileName"`
	BytesTransferred uint64 `json:"bytesTransferred"`
	TotalBytes       uint64 `json:"totalBytes"`
	CompletedFiles   int    `json:"completedFiles"`
	TotalFiles       int    `json:"totalFiles"`
}

// TransferEndEvent reports completion, PIN challenge, cancellation, or error of an outbound transfer.
type TransferEndEvent struct {
	TransferID  string `json:"transferId"`
	Status      string `json:"status"` // "finished" | "cancelled" | "pin_required" | "failed"
	Error       string `json:"error,omitempty"`
	PinRequired bool   `json:"pinRequired,omitempty"`
}

// CoordinatorCallbacks delivers outbound transfer lifecycle updates to IPC.
type CoordinatorCallbacks struct {
	OnProgress func(TransferProgressEvent)
	OnEnd      func(TransferEndEvent)
}

type preparedSource struct {
	offer    shared.FileOfferV2
	filePath string
	isText   bool
}

type activeSend struct {
	transferID string
	peerHost   string
	sessionID  string
	cancel     context.CancelFunc
}

// SendCoordinator manages outbound file/text transfers with cancellation and bounded concurrency.
type SendCoordinator struct {
	cert      *identity.DeviceCertificate
	callbacks CoordinatorCallbacks

	mu        sync.Mutex
	transfers map[string]*activeSend
}

// NewSendCoordinator creates a SendCoordinator.
func NewSendCoordinator(cert *identity.DeviceCertificate, callbacks CoordinatorCallbacks) *SendCoordinator {
	return &SendCoordinator{
		cert:      cert,
		callbacks: callbacks,
		transfers: make(map[string]*activeSend),
	}
}

// StartTransfer begins an asynchronous outbound transfer and returns its transferId immediately.
func (c *SendCoordinator) StartTransfer(
	settings identity.AppSettings,
	target TargetPeerEndpoint,
	items []StagedItemInput,
	pin string,
) (string, error) {
	if len(items) == 0 {
		return "", errors.New("no staged items to send")
	}
	addr, err := shared.ParsePeerAddr(target.Host)
	if err != nil {
		return "", fmt.Errorf("invalid target host %q: %w", target.Host, err)
	}
	sources, err := expandStagedSources(items)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", errors.New("selected directories contain no files")
	}

	transferID, err := shared.NewRandomUUIDv4()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	entry := &activeSend{
		transferID: transferID,
		peerHost:   addr.IP.String(),
		cancel:     cancel,
	}

	c.mu.Lock()
	c.transfers[transferID] = entry
	c.mu.Unlock()

	go c.runTransfer(ctx, entry, settings, target, addr, sources, pin)
	return transferID, nil
}

// CancelTransfer cancels an active outbound transfer by transferId.
func (c *SendCoordinator) CancelTransfer(transferID string) bool {
	c.mu.Lock()
	entry, ok := c.transfers[transferID]
	c.mu.Unlock()
	if !ok {
		return false
	}
	entry.cancel()
	return true
}

// CancelByRemoteSession cancels an outbound transfer when the receiver calls /cancel.
func (c *SendCoordinator) CancelByRemoteSession(peerIP string, sessionID string) {
	c.mu.Lock()
	var match *activeSend
	for _, entry := range c.transfers {
		if entry.peerHost == peerIP && entry.sessionID == sessionID {
			match = entry
			break
		}
	}
	c.mu.Unlock()
	if match != nil {
		match.cancel()
	}
}

func (c *SendCoordinator) runTransfer(
	ctx context.Context,
	entry *activeSend,
	settings identity.AppSettings,
	target TargetPeerEndpoint,
	addr shared.PeerAddr,
	sources map[string]preparedSource,
	pin string,
) {
	defer func() {
		entry.cancel()
		c.mu.Lock()
		delete(c.transfers, entry.transferID)
		c.mu.Unlock()
	}()

	expectedFP := ""
	if target.Protocol == shared.ProtocolHTTPS {
		expectedFP = target.Fingerprint
	}
	// Prepare-upload may wait up to 2 minutes for receiver user approval.
	prepareClient := BuildPeerHTTPClient(c.cert, expectedFP, 120*time.Second)

	selfProto := shared.ProtocolHTTP
	if settings.HTTPS {
		selfProto = shared.ProtocolHTTPS
	}

	offers := make(map[string]shared.FileOfferV2, len(sources))
	for id, src := range sources {
		offers[id] = src.offer
	}

	prepReq := shared.PrepareUploadRequestV2{
		Info: shared.RegisterRequestV2{
			Alias:       settings.Alias,
			Version:     shared.ProtocolVersionV2,
			DeviceModel: settings.DeviceModel,
			DeviceType:  shared.DeviceDesktop,
			Fingerprint: c.cert.Fingerprint,
			Port:        settings.Port,
			Protocol:    selfProto,
			Download:    false,
		},
		Files: offers,
	}

	prepResp, statusCode, err := SendPrepareUploadV2(ctx, prepareClient, target.Protocol, addr, target.Port, prepReq, pin)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			c.emitEnd(TransferEndEvent{TransferID: entry.transferID, Status: "cancelled"})
			return
		}
		if IsPinRequiredError(err) || statusCode == http.StatusUnauthorized {
			c.emitEnd(TransferEndEvent{
				TransferID:  entry.transferID,
				Status:      "pin_required",
				PinRequired: true,
				Error:       err.Error(),
			})
			return
		}
		c.emitEnd(TransferEndEvent{
			TransferID: entry.transferID,
			Status:     "failed",
			Error:      err.Error(),
		})
		return
	}

	// Text-only transfer completed at prepare-upload (HTTP 204).
	if statusCode == http.StatusNoContent || prepResp == nil || len(prepResp.Files) == 0 {
		c.emitEnd(TransferEndEvent{TransferID: entry.transferID, Status: "finished"})
		return
	}

	c.mu.Lock()
	entry.sessionID = prepResp.SessionID
	c.mu.Unlock()

	uploadClient := BuildPeerHTTPClient(c.cert, expectedFP, 0)
	type job struct {
		fileID string
		token  string
		src    preparedSource
	}
	jobs := make(chan job, len(prepResp.Files))
	for fileID, tok := range prepResp.Files {
		if src, ok := sources[fileID]; ok && !src.isText {
			jobs <- job{fileID: fileID, token: tok, src: src}
		}
	}
	close(jobs)

	totalFiles := len(prepResp.Files)
	var completedFiles atomic.Int32
	var firstErr error
	var wg sync.WaitGroup

	// Upload one file at a time: LocalSend on iOS resets additional concurrent
	// upload connections, and a single pooled connection serves all files.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := range jobs {
			if ctx.Err() != nil {
				return
			}
			if upErr := c.uploadOneFile(ctx, uploadClient, target, addr, prepResp.SessionID, j.fileID, j.token, j.src, &completedFiles, totalFiles, entry.transferID); upErr != nil {
				firstErr = upErr
				entry.cancel()
				return
			}
		}
	}()
	wg.Wait()

	if errors.Is(ctx.Err(), context.Canceled) && firstErr == nil {
		cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 2*time.Second)
		_ = SendCancelSessionV2(cancelCtx, prepareClient, target.Protocol, addr, target.Port, prepResp.SessionID)
		cancelTimeout()
		c.emitEnd(TransferEndEvent{TransferID: entry.transferID, Status: "cancelled"})
		return
	}
	if firstErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) && errors.Is(firstErr, context.Canceled) {
			cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 2*time.Second)
			_ = SendCancelSessionV2(cancelCtx, prepareClient, target.Protocol, addr, target.Port, prepResp.SessionID)
			cancelTimeout()
			c.emitEnd(TransferEndEvent{TransferID: entry.transferID, Status: "cancelled"})
			return
		}
		c.emitEnd(TransferEndEvent{
			TransferID: entry.transferID,
			Status:     "failed",
			Error:      firstErr.Error(),
		})
		return
	}

	c.emitEnd(TransferEndEvent{TransferID: entry.transferID, Status: "finished"})
}

func (c *SendCoordinator) uploadOneFile(
	ctx context.Context,
	client *http.Client,
	target TargetPeerEndpoint,
	addr shared.PeerAddr,
	sessionID string,
	fileID string,
	token string,
	src preparedSource,
	completedFiles *atomic.Int32,
	totalFiles int,
	transferID string,
) error {
	f, err := os.Open(src.filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", src.filePath, err)
	}
	defer f.Close()

	reader := &progressReader{
		r:          f,
		total:      src.offer.Size,
		lastReport: time.Now(),
		onTick: func(sent uint64) {
			if c.callbacks.OnProgress != nil {
				c.callbacks.OnProgress(TransferProgressEvent{
					TransferID:       transferID,
					FileID:           fileID,
					FileName:         src.offer.FileName,
					BytesTransferred: sent,
					TotalBytes:       src.offer.Size,
					CompletedFiles:   int(completedFiles.Load()),
					TotalFiles:       totalFiles,
				})
			}
		},
	}

	if err := SendUploadFileV2(ctx, client, target.Protocol, addr, target.Port, sessionID, fileID, token, int64(src.offer.Size), reader); err != nil {
		return err
	}

	done := int(completedFiles.Add(1))
	if c.callbacks.OnProgress != nil {
		c.callbacks.OnProgress(TransferProgressEvent{
			TransferID:       transferID,
			FileID:           fileID,
			FileName:         src.offer.FileName,
			BytesTransferred: src.offer.Size,
			TotalBytes:       src.offer.Size,
			CompletedFiles:   done,
			TotalFiles:       totalFiles,
		})
	}
	return nil
}

func (c *SendCoordinator) emitEnd(ev TransferEndEvent) {
	if c.callbacks.OnEnd != nil {
		c.callbacks.OnEnd(ev)
	}
}

type progressReader struct {
	r          io.Reader
	total      uint64
	readBytes  uint64
	lastReport time.Time
	onTick     func(uint64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.readBytes += uint64(n)
		if p.onTick != nil && time.Since(p.lastReport) >= 100*time.Millisecond {
			p.onTick(p.readBytes)
			p.lastReport = time.Now()
		}
	}
	return n, err
}

func expandStagedSources(items []StagedItemInput) (map[string]preparedSource, error) {
	out := make(map[string]preparedSource)
	idx := 0
	for _, item := range items {
		switch item.Kind {
		case StagedText:
			idx++
			id := fmt.Sprintf("text-%d", idx)
			name := item.Name
			if name == "" {
				name = fmt.Sprintf("message-%d.txt", idx)
			}
			out[id] = preparedSource{
				isText: true,
				offer: shared.FileOfferV2{
					ID:       id,
					FileName: name,
					Size:     uint64(len(item.Text)),
					FileType: "text/plain",
					Preview:  item.Text,
				},
			}
		case StagedFile:
			info, err := os.Stat(item.Path)
			if err != nil {
				return nil, fmt.Errorf("stat %q: %w", item.Path, err)
			}
			if !info.IsDir() {
				idx++
				id := fmt.Sprintf("file-%d", idx)
				src, err := prepareSingleFileSource(id, item.Path, filepath.Base(item.Path), info)
				if err != nil {
					return nil, err
				}
				out[id] = src
				continue
			}
			rootParent := filepath.Dir(item.Path)
			walkErr := filepath.WalkDir(item.Path, func(p string, d os.DirEntry, wErr error) error {
				if wErr != nil {
					return wErr
				}
				if d.IsDir() {
					return nil
				}
				fi, fiErr := d.Info()
				if fiErr != nil {
					return fiErr
				}
				rel, relErr := filepath.Rel(rootParent, p)
				if relErr != nil {
					rel = d.Name()
				}
				idx++
				id := fmt.Sprintf("file-%d", idx)
				src, prepErr := prepareSingleFileSource(id, p, filepath.ToSlash(rel), fi)
				if prepErr != nil {
					return prepErr
				}
				out[id] = src
				return nil
			})
			if walkErr != nil {
				return nil, walkErr
			}
		default:
			return nil, fmt.Errorf("unknown staged item kind %q", item.Kind)
		}
	}
	return out, nil
}

func prepareSingleFileSource(id string, absPath string, offerName string, info os.FileInfo) (preparedSource, error) {
	hashHex, err := computeFileSHA256(absPath)
	if err != nil {
		return preparedSource{}, err
	}
	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return preparedSource{
		filePath: absPath,
		offer: shared.FileOfferV2{
			ID:       id,
			FileName: offerName,
			Size:     uint64(info.Size()),
			FileType: mimeType,
			SHA256:   hashHex,
			Metadata: &shared.FileMetadata{
				Modified: info.ModTime().UTC().Format(time.RFC3339Nano),
			},
		},
	}, nil
}

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash open %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash read %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
