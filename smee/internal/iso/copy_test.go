package iso

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/tinkerbell/smee/internal/iso/internal"
)

func TestCopyPatchFit(t *testing.T) {
	const magic = "MAGIC-0123456789"
	tests := map[string]struct {
		patch string
		want  string
	}{
		"fits, padded with spaces": {
			patch: "a=1 b=2",
			want:  "linux /kernel a=1 b=2" + strings.Repeat(" ", len(magic)-len("a=1 b=2")) + "\ninitrd /initrd\n",
		},
		"exactly fills the slot": {
			patch: "a=1 b=2 c=3 d=45",
			want:  "linux /kernel a=1 b=2 c=3 d=45\ninitrd /initrd\n",
		},
		// Patching would overwrite the line that follows, so the source is
		// left as is.
		"longer than the slot": {
			patch: "a=1 b=2 c=3 d=456",
			want:  "linux /kernel " + magic + "\ninitrd /initrd\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := &Handler{Logger: logr.Discard(), Patch: Patch{MagicString: magic}}
			h.Patch.magicStrPadding = bytes.Repeat([]byte{' '}, len(magic))
			ctx := internal.WithPatch(context.Background(), []byte(tt.patch))
			src := strings.NewReader("linux /kernel " + magic + "\ninitrd /initrd\n")

			var dst bytes.Buffer
			if _, err := h.Copy(ctx, &dst, src, nil); err != nil {
				t.Fatal(err)
			}
			if got := dst.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
