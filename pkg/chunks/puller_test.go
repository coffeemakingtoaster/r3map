package chunks

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestPuller(t *testing.T) {
	tests := []struct {
		name         string
		chunkSize    int64
		chunks       int64
		workers      int64
		data         [][]byte
		pullPriority func(offset int64) int64
		dirtyOffsets []int64
	}{
		{
			name:      "Pull 1 chunk with 1 worker and generic pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 1 chunk with 2 workers and generic pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 1 worker and generic pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 2 workers and generic pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 1 chunk with 1 worker and linear pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 1 chunk with 2 workers and linear pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 1 worker and linear pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 2 workers and linear pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 1 chunk with 1 worker and decreasing pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return -offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 1 chunk with 2 workers and decreasing pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test")},
			pullPriority: func(offset int64) int64 {
				return -offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 1 worker and decreasing pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   1,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return -offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull 2 chunks with 2 workers and decreasing pull priority heuristic",
			chunkSize: 4,
			chunks:    2,
			workers:   2,
			data:      [][]byte{[]byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return -offset
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull with no chunks finalization",
			chunkSize: 4,
			chunks:    3,
			workers:   1,
			data:      [][]byte{[]byte("test"), []byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{},
		},
		{
			name:      "Pull with some chunks finalization",
			chunkSize: 4,
			chunks:    3,
			workers:   2,
			data:      [][]byte{[]byte("test"), []byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{8},
		},
		{
			name:      "Pull with all chunks finalization",
			chunkSize: 4,
			chunks:    3,
			workers:   2,
			data:      [][]byte{[]byte("test"), []byte("test"), []byte("test")},
			pullPriority: func(offset int64) int64 {
				return 1
			},
			dirtyOffsets: []int64{0, 4, 8},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			remoteFile, err := os.CreateTemp("", "")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(remoteFile.Name())

			if err := remoteFile.Truncate(tc.chunkSize * tc.chunks); err != nil {
				t.Fatal(err)
			}

			localFile, err := os.CreateTemp("", "")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(localFile.Name())

			if err := localFile.Truncate(tc.chunkSize * tc.chunks); err != nil {
				t.Fatal(err)
			}

			remote := NewChunkedReadWriterAt(remoteFile, tc.chunkSize, tc.chunks)

			for i, chunk := range tc.data {
				if _, werr := remote.WriteAt(chunk, int64(i)*tc.chunkSize); werr != nil {
					t.Fatal(err)
				}
			}

			local := NewChunkedReadWriterAt(localFile, tc.chunkSize, tc.chunks)

			srw := NewSyncedReadWriterAt(remote, local, func(off int64) error {
				return nil
			})

			ctx := context.Background()

			puller := NewPuller(
				ctx,
				srw,
				tc.chunkSize,
				tc.chunks,
				tc.pullPriority,
			)
			err = puller.Open(tc.workers)
			if err != nil {
				t.Fatal(err)
			}

			puller.FinalizePull(tc.dirtyOffsets)

			if err := puller.Wait(); err != nil {
				t.Fatal(err)
			}

			if err := puller.Close(); err != nil {
				t.Fatal(err)
			}

			for i, chunk := range tc.data {
				localData := make([]byte, len(chunk))
				if _, err := local.ReadAt(localData, int64(i)*tc.chunkSize); err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(localData, chunk) {
					t.Errorf("Data pulled did not match expected. got %v, want %v", localData, tc.data)
				}
			}
		})
	}
}
