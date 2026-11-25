package shared

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// ProtocolVersionV2 is the LocalSend v2.2 wire protocol version.
	ProtocolVersionV2 = "2.2"
	// DefaultPort is the default TCP/UDP port used by LocalSend.
	DefaultPort uint16 = 53317
	// DefaultMulticastGroupV4 is the default IPv4 multicast address.
	DefaultMulticastGroupV4 = "224.0.0.167"
	// DefaultMulticastGroupV6 is the link-local IPv6 multicast group extension.
	DefaultMulticastGroupV6 = "ff12::fd3a:e420"
)

// ProtocolType represents HTTP or HTTPS transport for LocalSend v2.
type ProtocolType string

const (
	ProtocolHTTP  ProtocolType = "http"
	ProtocolHTTPS ProtocolType = "https"
)

// UnmarshalJSON validates protocol values on the v2 wire.
func (p *ProtocolType) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch strings.ToLower(raw) {
	case string(ProtocolHTTP):
		*p = ProtocolHTTP
	case string(ProtocolHTTPS):
		*p = ProtocolHTTPS
	default:
		return fmt.Errorf("unknown protocol %q", raw)
	}
	return nil
}

// DeviceType represents a LocalSend peer category.
type DeviceType string

const (
	DeviceMobile   DeviceType = "mobile"
	DeviceDesktop  DeviceType = "desktop"
	DeviceWeb      DeviceType = "web"
	DeviceHeadless DeviceType = "headless"
	DeviceServer   DeviceType = "server"
)

// UnmarshalJSON falls back unknown device types to "desktop" per protocol section 7.1.
func (d *DeviceType) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch strings.ToLower(raw) {
	case string(DeviceMobile):
		*d = DeviceMobile
	case string(DeviceDesktop):
		*d = DeviceDesktop
	case string(DeviceWeb):
		*d = DeviceWeb
	case string(DeviceHeadless):
		*d = DeviceHeadless
	case string(DeviceServer):
		*d = DeviceServer
	default:
		*d = DeviceDesktop
	}
	return nil
}

// MulticastMessageV2 is the UDP multicast discovery payload.
type MulticastMessageV2 struct {
	Alias       string       `json:"alias"`
	Version     string       `json:"version"`
	DeviceModel string       `json:"deviceModel,omitempty"`
	DeviceType  DeviceType   `json:"deviceType,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	Port        uint16       `json:"port"`
	Protocol    ProtocolType `json:"protocol"`
	Download    bool         `json:"download"`
}

// AnnouncedMessageV2 adds the legacy v2 announce flag for outgoing multicast bursts.
type AnnouncedMessageV2 struct {
	Alias       string       `json:"alias"`
	Version     string       `json:"version"`
	DeviceModel string       `json:"deviceModel,omitempty"`
	DeviceType  DeviceType   `json:"deviceType,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	Port        uint16       `json:"port"`
	Protocol    ProtocolType `json:"protocol"`
	Download    bool         `json:"download"`
	Announce    bool         `json:"announce"`
}

// RegisterRequestV2 is sent to POST /api/localsend/v2/register.
type RegisterRequestV2 struct {
	Alias       string       `json:"alias"`
	Version     string       `json:"version"`
	DeviceModel string       `json:"deviceModel,omitempty"`
	DeviceType  DeviceType   `json:"deviceType,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	Port        uint16       `json:"port"`
	Protocol    ProtocolType `json:"protocol"`
	Download    bool         `json:"download"`
}

// RegisterResponseV2 is returned from POST /api/localsend/v2/register.
type RegisterResponseV2 struct {
	Alias       string     `json:"alias"`
	Version     string     `json:"version"`
	DeviceModel string     `json:"deviceModel,omitempty"`
	DeviceType  DeviceType `json:"deviceType,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	Download    bool       `json:"download"`
}

// InfoResponseV2 is returned from GET /api/localsend/v2/info.
type InfoResponseV2 struct {
	Alias       string     `json:"alias"`
	Version     string     `json:"version"`
	DeviceModel string     `json:"deviceModel,omitempty"`
	DeviceType  DeviceType `json:"deviceType,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	Download    bool       `json:"download"`
}

// FileMetadata carries optional RFC 3339 file timestamps.
type FileMetadata struct {
	Modified string `json:"modified,omitempty"`
	Accessed string `json:"accessed,omitempty"`
}

// FileOfferV2 describes one file or text item in a v2 transfer offer.
type FileOfferV2 struct {
	ID       string        `json:"id"`
	FileName string        `json:"fileName"`
	Size     uint64        `json:"size"`
	FileType string        `json:"fileType"`
	SHA256   string        `json:"sha256,omitempty"`
	Preview  string        `json:"preview,omitempty"`
	Metadata *FileMetadata `json:"metadata,omitempty"`
}

// PrepareUploadRequestV2 is sent to POST /api/localsend/v2/prepare-upload.
type PrepareUploadRequestV2 struct {
	Info  RegisterRequestV2      `json:"info"`
	Files map[string]FileOfferV2 `json:"files"`
}

// PrepareUploadResponseV2 is returned from POST /api/localsend/v2/prepare-upload.
type PrepareUploadResponseV2 struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"`
}

// IsTextOnlyOffer reports whether an offered item carries inline text without binary upload.
func IsTextOnlyOffer(offer FileOfferV2) bool {
	return offer.FileType == "text/plain" && offer.Preview != ""
}
