package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"nshsend/core/internal/diaglog"
	"nshsend/core/internal/discovery"
	"nshsend/core/internal/identity"
	"nshsend/core/internal/ipc"
	"nshsend/core/internal/receive"
	"nshsend/core/internal/send"
	"nshsend/core/internal/shared"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stderr)

	defaultDataDir := ""
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		defaultDataDir = filepath.Join(cfgDir, "nshsend")
	} else {
		defaultDataDir = filepath.Join(os.TempDir(), "nshsend")
	}

	dataDirFlag := flag.String("data-dir", defaultDataDir, "directory for persisted identity, settings, and history")
	flag.Parse()

	diagLog, logErr := diaglog.OpenDiagnosticLog(*dataDirFlag)
	if logErr == nil {
		log.SetOutput(diagLog)
		defer diagLog.Close()
	} else {
		log.Printf("diagnostic log file disabled: %v", logErr)
	}

	cert, err := identity.LoadOrCreateDeviceCertificate(*dataDirFlag)
	if err != nil {
		log.Fatalf("load device certificate: %v", err)
	}
	settingsStore, err := identity.LoadSettingsStore(*dataDirFlag)
	if err != nil {
		log.Fatalf("load settings: %v", err)
	}
	historyStore, err := receive.LoadHistoryStore(*dataDirFlag)
	if err != nil {
		log.Fatalf("load history: %v", err)
	}

	writer := ipc.NewTransportWriter(os.Stdout)
	deviceStore := discovery.NewDeviceStore()

	sendCoord := send.NewSendCoordinator(cert, send.CoordinatorCallbacks{
		OnProgress: func(ev send.TransferProgressEvent) {
			_ = writer.WriteEvent("send.progress", ev)
		},
		OnEnd: func(ev send.TransferEndEvent) {
			_ = writer.WriteEvent("send.ended", ev)
		},
	})

	initialSettings := settingsStore.CurrentSettings()
	discCoord := discovery.NewDiscoveryCoordinator(
		deviceStore,
		cert,
		initialSettings,
		func(ctx context.Context, protocol shared.ProtocolType, addr shared.PeerAddr, port uint16, expectedFP string, selfInfo shared.RegisterRequestV2) (shared.RegisterResponseV2, string, error) {
			return send.SendRegisterRequestV2(ctx, cert, protocol, addr, port, expectedFP, selfInfo)
		},
		func(devices []discovery.PeerDeviceView) {
			_ = writer.WriteEvent("devices.updated", devices)
		},
	)

	recvServer := receive.NewReceiveServer(cert, initialSettings, historyStore, receive.ServerCallbacks{
		OnRegister: func(addr shared.PeerAddr, info shared.RegisterRequestV2) {
			discCoord.NotifyInboundRegistration(addr, info)
		},
		OnIncomingOffer: func(offer receive.IncomingOfferEvent) {
			_ = writer.WriteEvent("receive.incoming", offer)
		},
		OnOfferAborted: func(sessionID string) {
			_ = writer.WriteEvent("receive.aborted", map[string]string{"sessionId": sessionID})
		},
		OnProgress: func(progress receive.ProgressEvent) {
			_ = writer.WriteEvent("receive.progress", progress)
		},
		OnSessionEnd: func(end receive.SessionEndEvent) {
			_ = writer.WriteEvent("receive.ended", end)
		},
		OnRemoteCancel: func(peerIP string, sessionID string) {
			sendCoord.CancelByRemoteSession(peerIP, sessionID)
		},
		OnHistoryUpdated: func(entries []receive.HistoryEntry) {
			_ = writer.WriteEvent("history.updated", entries)
		},
	})

	router := ipc.NewRouter(writer, cert, settingsStore, deviceStore, discCoord, recvServer, sendCoord, historyStore)
	if diagLog != nil {
		router.SetLogReader(diagLog)
	}

	if boundPort, srvErr := recvServer.StartServer(initialSettings); srvErr != nil {
		log.Printf("receive server failed to bind port %d: %v", initialSettings.Port, srvErr)
		router.UpdateServerState(false, initialSettings.Port, initialSettings.HTTPS, srvErr.Error())
	} else {
		router.UpdateServerState(true, boundPort, initialSettings.HTTPS, "")
	}

	if mcErr := discCoord.StartMulticast(initialSettings); mcErr != nil {
		log.Printf("multicast discovery unavailable: %v", mcErr)
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		<-ctx.Done()
		_ = os.Stdin.Close()
	}()

	_ = ipc.ReadRequestFrames(os.Stdin, func(frame ipc.RequestFrame) {
		go router.DispatchFrame(ctx, frame)
	})

	discCoord.StopMulticast()
	recvServer.StopServer()
}
