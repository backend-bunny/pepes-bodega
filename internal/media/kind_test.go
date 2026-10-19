package media

import "testing"

func TestDetect(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	gif := []byte("GIF89a\x01\x00\x01\x00")
	webm := []byte("\x1aE\xdf\xa3\x9fB\x86\x81\x01")
	cases := []struct {
		name     string
		head     []byte
		file     string
		wantKind string
		wantOK   bool
	}{
		{"png", png, "a.png", KindImage, true},
		{"gif is animation", gif, "a.gif", KindAnimation, true},
		{"webm", webm, "a.webm", KindVideo, true},
		{"unknown bytes, known ext", []byte{0, 1, 2, 3}, "clip.mov", KindVideo, true},
		{"text", []byte("hello world"), "notes.txt", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, kind, ok := Detect(c.head, c.file)
			if ok != c.wantOK || kind != c.wantKind {
				t.Fatalf("Detect = (%q, %v), want (%q, %v)", kind, ok, c.wantKind, c.wantOK)
			}
		})
	}
}
