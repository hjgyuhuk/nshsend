package receive

import (
	"nshsend/core/internal/shared"
)

// FileStatus tracks the transfer state of one accepted file in an upload session.
type FileStatus uint8

const (
	FileStatusPending FileStatus = iota
	FileStatusInProgress
	FileStatusFinished
	FileStatusFailed
)

// SessionFile holds the offer, upload token, and status for one file.
type SessionFile struct {
	Offer  shared.FileOfferV2
	Token  string
	Status FileStatus
}

// PendingOfferDecision is the application's response to an incoming prepare-upload offer.
type PendingOfferDecision struct {
	AcceptedIDs map[string]struct{}
	Declined    bool
}

type pendingSession struct {
	sessionID  string
	senderIP   string
	decisionCh chan PendingOfferDecision
	cancelCh   chan struct{}
}

type activeSession struct {
	sessionID   string
	senderIP    string
	senderAlias string
	files       map[string]*SessionFile
}

type sessionSlot struct {
	pending *pendingSession
	active  *activeSession
}

func (s *sessionSlot) isBusy() bool {
	return s.pending != nil || s.active != nil
}
