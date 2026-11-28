package taskboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkTaskStoreUpdate(b *testing.B) {
	for _, count := range []int{8, 128, 512} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			store := Store{filepath.Join(b.TempDir(), "private")}
			if err := os.Mkdir(store.Root, 0700); err != nil {
				b.Fatal(err)
			}
			board := boardFile{Version: 1, Tasks: make([]Task, count)}
			for i := range board.Tasks {
				board.Tasks[i] = Task{
					ID:   fmt.Sprintf("%032x", i),
					Base: strings.Repeat("a", 40),
					Spec: Spec{Title: "fixture task", Prompt: strings.Repeat("review change\n", 48), Agent: "shell"},
				}
			}
			data, err := json.Marshal(board)
			if err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(store.path(), data, 0600); err != nil {
				b.Fatal(err)
			}
			id := board.Tasks[count-1].ID
			b.ReportAllocs()
			for b.Loop() {
				if _, err := store.Update(id, func(task *Task) error {
					task.State = "queued"
					return nil
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
