package workspacecopy_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pacificbelt30/codex-dock/internal/workspacecopy"
)

func BenchmarkWorkspaceCopy(b *testing.B) {
	for _, fixture := range []struct{ files, size int }{{512, 1024}, {32, 256 << 10}} {
		b.Run(fmt.Sprintf("files=%d/size=%d", fixture.files, fixture.size), func(b *testing.B) {
			root := b.TempDir()
			data := make([]byte, fixture.size)
			for i := range fixture.files {
				dir := filepath.Join(root, "project", fmt.Sprintf("dir-%02d", i/16))
				if err := os.MkdirAll(dir, 0700); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%04d", i)), data, 0600); err != nil {
					b.Fatal(err)
				}
			}
			const limit = 32 << 20
			plan, err := workspacecopy.Prepare(b.Context(), root, limit)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Review", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(fixture.files * fixture.size))
				for b.Loop() {
					if _, err := workspacecopy.Prepare(b.Context(), root, limit); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Archive", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(fixture.files * fixture.size))
				for b.Loop() {
					if err := plan.Write(b.Context(), io.Discard); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
