package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"nshsend/core/internal/desktopdialog"
	"nshsend/core/internal/discovery"
	"nshsend/core/internal/identity"
	"nshsend/core/internal/receive"
	"nshsend/core/internal/send"
	"nshsend/core/internal/shared"
)

// ServerStateSnapshot describes the active HTTP/HTTPS receiver status.
type ServerStateSnapshot struct {
	Running        bool     `json:"running"`
	Port           uint16   `json:"port"`
	HTTPS          bool     `json:"https"`
	LocalAddresses []string `json:"localAddresses"`
	Error          string   `json:"error,omitempty"`
}

// BootstrapResult is returned by core.bootstrap with complete initial state.
type BootstrapResult struct {
	Fingerprint string                   `json:"fingerprint"`
	Settings    identity.AppSettings     `json:"settings"`
	Server      ServerStateSnapshot      `json:"server"`
	Devices     []discovery.PeerDeviceView `json:"devices"`
	History     []receive.HistoryEntry   `json:"history"`
}

// LogReader exposes the diagnostic log file path and recent log lines.
type LogReader interface {
	FilePath() string
	RecentLines(limit int) []string
}

// Router dispatches NDJSON IPC requests to the Go core domain modules.
type Router struct {
	writer        *TransportWriter
	cert          *identity.DeviceCertificate
	settingsStore *identity.SettingsStore
	deviceStore   *discovery.DeviceStore
	discCoord     *discovery.DiscoveryCoordinator
	recvServer    *receive.ReceiveServer
	sendCoord     *send.SendCoordinator
	historyStore  *receive.HistoryStore
	logReader     LogReader

	srvMu       sync.Mutex
	serverState ServerStateSnapshot
}

// SetLogReader attaches a diagnostic log reader for logs.tail queries.
func (r *Router) SetLogReader(reader LogReader) {
	r.logReader = reader
}

// NewRouter constructs an IPC Router wired to all core domain services.
func NewRouter(
	writer *TransportWriter,
	cert *identity.DeviceCertificate,
	settingsStore *identity.SettingsStore,
	deviceStore *discovery.DeviceStore,
	discCoord *discovery.DiscoveryCoordinator,
	recvServer *receive.ReceiveServer,
	sendCoord *send.SendCoordinator,
	historyStore *receive.HistoryStore,
) *Router {
	return &Router{
		writer:        writer,
		cert:          cert,
		settingsStore: settingsStore,
		deviceStore:   deviceStore,
		discCoord:     discCoord,
		recvServer:    recvServer,
		sendCoord:     sendCoord,
		historyStore:  historyStore,
	}
}

// UpdateServerState records the latest HTTP/mTLS listener status and notifies the UI.
func (r *Router) UpdateServerState(running bool, port uint16, https bool, errMsg string) {
	snap := ServerStateSnapshot{
		Running:        running,
		Port:           port,
		HTTPS:          https,
		LocalAddresses: discovery.ListLocalNonLoopbackIPs(),
		Error:          errMsg,
	}
	r.srvMu.Lock()
	r.serverState = snap
	r.srvMu.Unlock()
	_ = r.writer.WriteEvent("server.state", snap)
}

// DispatchFrame executes a single RequestFrame and writes its ResponseFrame.
func (r *Router) DispatchFrame(ctx context.Context, frame RequestFrame) {
	res, err := r.routeMethod(ctx, frame.Method, frame.Params)
	if err != nil {
		_ = r.writer.WriteResponse(ResponseFrame{
			ID: frame.ID,
			Error: &ResponseError{
				Code:    "CORE_ERROR",
				Message: err.Error(),
			},
		})
		return
	}
	_ = r.writer.WriteResponse(ResponseFrame{
		ID:     frame.ID,
		Result: res,
	})
}

func (r *Router) routeMethod(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "core.bootstrap":
		r.srvMu.Lock()
		srv := r.serverState
		r.srvMu.Unlock()
		return BootstrapResult{
			Fingerprint: r.cert.Fingerprint,
			Settings:    r.settingsStore.CurrentSettings(),
			Server:      srv,
			Devices:     r.deviceStore.ConfirmedDevices(),
			History:     r.historyStore.ListHistoryEntries(),
		}, nil

	case "discovery.scan":
		if err := r.discCoord.ScanPeers(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil

	case "discovery.registerManual":
		var params struct {
			Host     string              `json:"host"`
			Port     uint16              `json:"port"`
			Protocol shared.ProtocolType `json:"protocol"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid registerManual params: %w", err)
		}
		return r.discCoord.RegisterManualPeer(ctx, params.Host, params.Port, params.Protocol)

	case "send.start":
		var params struct {
			TargetFingerprint string                 `json:"targetFingerprint"`
			Host              string                 `json:"host"`
			Port              uint16                 `json:"port"`
			Protocol          shared.ProtocolType    `json:"protocol"`
			Items             []send.StagedItemInput `json:"items"`
			PIN               string                 `json:"pin"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid send.start params: %w", err)
		}
		target := send.TargetPeerEndpoint{
			Fingerprint: params.TargetFingerprint,
			Host:        params.Host,
			Port:        params.Port,
			Protocol:    params.Protocol,
		}
		if target.Host == "" && params.TargetFingerprint != "" {
			if dev, channels, ok := r.deviceStore.LookupDevice(params.TargetFingerprint); ok {
				target.Host = dev.Host
				target.Port = dev.Port
				target.Protocol = dev.Protocol
				if len(channels) > 0 {
					target.Host = channels[0].Host
					target.Port = channels[0].Port
					target.Protocol = channels[0].Protocol
				}
			}
		}
		transferID, err := r.sendCoord.StartTransfer(r.settingsStore.CurrentSettings(), target, params.Items, params.PIN)
		if err != nil {
			return nil, err
		}
		return map[string]string{"transferId": transferID}, nil

	case "send.cancel":
		var params struct {
			TransferID string `json:"transferId"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid send.cancel params: %w", err)
		}
		return map[string]bool{"cancelled": r.sendCoord.CancelTransfer(params.TransferID)}, nil

	case "receive.respond":
		var params struct {
			SessionID       string   `json:"sessionId"`
			Accept          bool     `json:"accept"`
			AcceptedFileIDs []string `json:"acceptedFileIds"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid receive.respond params: %w", err)
		}
		if err := r.recvServer.RespondToIncomingOffer(params.SessionID, params.Accept, params.AcceptedFileIDs); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil

	case "receive.cancel":
		var params struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid receive.cancel params: %w", err)
		}
		return map[string]bool{"cancelled": r.recvServer.CancelIncomingSession(params.SessionID)}, nil

	case "settings.update":
		var patch identity.SettingsPatch
		if err := json.Unmarshal(raw, &patch); err != nil {
			return nil, fmt.Errorf("invalid settings.update params: %w", err)
		}
		updated, needsRestart, err := r.settingsStore.UpdateSettings(patch)
		if err != nil {
			return nil, err
		}
		if needsRestart {
			r.restartNetworking(updated)
		}
		return updated, nil

	case "history.list":
		return r.historyStore.ListHistoryEntries(), nil

	case "history.remove":
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid history.remove params: %w", err)
		}
		if err := r.historyStore.RemoveHistoryEntry(params.ID); err != nil {
			return nil, err
		}
		return r.historyStore.ListHistoryEntries(), nil

	case "history.clear":
		if err := r.historyStore.ClearHistoryEntries(); err != nil {
			return nil, err
		}
		return []receive.HistoryEntry{}, nil

	case "dialog.pickFiles":
		var params struct {
			Multiple bool `json:"multiple"`
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &params)
		}
		paths, err := desktopdialog.PickFilesNative(ctx, params.Multiple)
		if err != nil {
			return nil, err
		}
		return map[string][]string{"paths": paths}, nil

	case "dialog.pickFolder":
		path, err := desktopdialog.PickFolderNative(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": path}, nil

	case "system.openPath":
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid system.openPath params: %w", err)
		}
		if err := desktopdialog.OpenLocalPath(params.Path); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil

	case "system.revealPath":
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, fmt.Errorf("invalid system.revealPath params: %w", err)
		}
		if err := desktopdialog.RevealLocalPath(params.Path); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil

	case "logs.tail":
		var params struct {
			Limit int `json:"limit"`
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &params)
		}
		limit := params.Limit
		if limit <= 0 {
			limit = 40
		}
		if r.logReader == nil {
			return map[string]any{"logFilePath": "", "lines": []string{}}, nil
		}
		return map[string]any{
			"logFilePath": r.logReader.FilePath(),
			"lines":       r.logReader.RecentLines(limit),
		}, nil

	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (r *Router) restartNetworking(settings identity.AppSettings) {
	port, err := r.recvServer.StartServer(settings)
	if err != nil {
		r.UpdateServerState(false, settings.Port, settings.HTTPS, err.Error())
	} else {
		r.UpdateServerState(true, port, settings.HTTPS, "")
	}
	_ = r.discCoord.StartMulticast(settings)
}
