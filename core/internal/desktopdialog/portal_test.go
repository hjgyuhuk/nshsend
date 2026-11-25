package desktopdialog

import (
	"reflect"
	"testing"
)

func encodePortalResponseForTest(code uint32, uris []string) []byte {
	var e dbusEncoder
	e.uint32(code)
	var entries []dbusDictEntry
	if uris != nil {
		entries = append(entries, dbusDictEntry{key: "uris", sig: "as", value: uris})
	}
	e.value("a{sv}", entries)
	return e.buf
}

func TestPortalRequestPathForToken(t *testing.T) {
	got := portalRequestPathForToken(":1.1105", "nshsendabc123")
	want := "/org/freedesktop/portal/desktop/request/1_1105/nshsendabc123"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodePortalResponseBody(t *testing.T) {
	cases := []struct {
		name    string
		body    []byte
		want    []string
		wantErr bool
	}{
		{
			name: "multiple files",
			body: encodePortalResponseForTest(0, []string{"file:///home/das/a.txt", "file:///home/das/b.png"}),
			want: []string{"/home/das/a.txt", "/home/das/b.png"},
		},
		{
			name: "directory selection",
			body: encodePortalResponseForTest(0, []string{"file:///home/das/Downloads"}),
			want: []string{"/home/das/Downloads"},
		},
		{
			name: "percent-encoded spaces and Chinese",
			body: encodePortalResponseForTest(0, []string{"file:///home/das/%E6%88%91%E7%9A%84%20%E6%96%87%E4%BB%B6.txt"}),
			want: []string{"/home/das/我的 文件.txt"},
		},
		{
			name: "path with single quote",
			body: encodePortalResponseForTest(0, []string{"file:///home/das/it's.txt"}),
			want: []string{"/home/das/it's.txt"},
		},
		{
			name: "cancelled",
			body: encodePortalResponseForTest(1, nil),
			want: nil,
		},
		{
			name:    "error response",
			body:    encodePortalResponseForTest(2, nil),
			wantErr: true,
		},
		{
			name: "success without uris key",
			body: encodePortalResponseForTest(0, nil),
			want: nil,
		},
		{
			name:    "bad percent encoding",
			body:    encodePortalResponseForTest(0, []string{"file:///home/das/%zz.txt"}),
			wantErr: true,
		},
		{
			name:    "truncated body",
			body:    []byte{0x00},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodePortalResponseBody(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestPortalURIToPath(t *testing.T) {
	got, err := portalURIToPath("https://example.com/x")
	if err != nil || got != "" {
		t.Fatalf("non-file URI: got %q err %v", got, err)
	}
	got, err = portalURIToPath("file:///home/das/a%20b.txt")
	if err != nil || got != "/home/das/a b.txt" {
		t.Fatalf("got %q err %v", got, err)
	}
}