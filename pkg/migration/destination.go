package migration

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/pojntfx/go-nbd/pkg/backend"
	"github.com/pojntfx/go-nbd/pkg/client"
	"github.com/pojntfx/go-nbd/pkg/server"
	bbackend "github.com/pojntfx/r3map/pkg/backend"
	"github.com/pojntfx/r3map/pkg/chunks"
	"github.com/pojntfx/r3map/pkg/device"
	"github.com/pojntfx/r3map/pkg/utils"
)

type Options struct {
	ChunkSize int64

	PullWorkers int64

	Verbose bool
}

type Hooks struct {
	OnChunkIsLocal func(off int64) error
}

type Destination struct {
	ctx context.Context

	remote io.ReaderAt
	size   int64

	flush func() ([]int64, error)

	local,
	syncer backend.Backend

	options *Options
	hooks   *Hooks

	serverOptions *server.Options
	clientOptions *client.Options

	serverFile       *os.File
	syncedReadWriter *chunks.SyncedReadWriterAt
	puller           *chunks.Puller
	dev              *device.Device

	wg   sync.WaitGroup
	errs chan error
}

func NewDestination(
	ctx context.Context,

	remote io.ReaderAt,
	size int64,

	flush func() ([]int64, error),

	local backend.Backend,

	options *Options,
	hooks *Hooks,

	serverOptions *server.Options,
	clientOptions *client.Options,
) *Destination {
	if options == nil {
		options = &Options{}
	}

	if options.ChunkSize <= 0 {
		options.ChunkSize = 4096
	}

	if options.PullWorkers <= 0 {
		options.PullWorkers = 512
	}

	if hooks == nil {
		hooks = &Hooks{}
	}

	return &Destination{
		ctx: ctx,

		remote: remote,
		size:   size,

		flush: flush,

		local: local,

		options: options,
		hooks:   hooks,

		serverOptions: serverOptions,
		clientOptions: clientOptions,

		errs: make(chan error),
	}
}

func (m *Destination) Wait() error {
	for err := range m.errs {
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Destination) Open() (string, error) {
	chunkCount := m.size / m.options.ChunkSize

	devicePath, err := utils.FindUnusedNBDDevice()
	if err != nil {
		return "", err
	}

	m.serverFile, err = os.Open(devicePath)
	if err != nil {
		return "", err
	}

	local := chunks.NewChunkedReadWriterAt(m.local, m.options.ChunkSize, chunkCount)

	hook := m.hooks.OnChunkIsLocal
	m.syncedReadWriter = chunks.NewSyncedReadWriterAt(m.remote, local, func(off int64) error {
		if hook != nil {
			return hook(off)
		}

		return nil
	})

	m.puller = chunks.NewPuller(
		m.ctx,
		m.syncedReadWriter,
		m.options.ChunkSize,
		chunkCount,
		func(offset int64) int64 {
			return 1
		},
	)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		if err := m.puller.Wait(); err != nil {
			m.errs <- err

			return
		}
	}()

	if err := m.puller.Open(m.options.PullWorkers); err != nil {
		return "", err
	}

	arbitraryReadWriter := chunks.NewArbitraryReadWriterAt(m.syncedReadWriter, m.options.ChunkSize)

	m.syncer = bbackend.NewReaderAtBackend(
		arbitraryReadWriter,
		func() (int64, error) {
			return m.size, nil
		},
		func() error {
			if err := m.local.Sync(); err != nil {
				return err
			}

			return nil
		},
		m.options.Verbose,
	)

	m.dev = device.NewDevice(
		m.syncer,
		m.serverFile,

		m.serverOptions,
		m.clientOptions,
	)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		if err := m.dev.Wait(); err != nil {
			m.errs <- err

			return
		}
	}()

	if err := m.dev.Open(); err != nil {
		return "", err
	}

	return devicePath, nil
}

func (m *Destination) FinalizePull() error {
	dirtyOffsets, err := m.flush()
	if err != nil {
		return err
	}

	if m.syncedReadWriter != nil {
		m.syncedReadWriter.MarkAsRemote(dirtyOffsets)
	}

	if m.puller != nil {
		m.puller.FinalizePull(dirtyOffsets)
	}

	return nil
}

func (m *Destination) Close() error {
	if m.syncer != nil {
		_ = m.syncer.Sync()
	}

	if m.dev != nil {
		_ = m.dev.Close()
	}

	if m.puller != nil {
		_ = m.puller.Close()
	}

	if m.serverFile != nil {
		_ = m.serverFile.Close()
	}

	m.wg.Wait()

	close(m.errs)

	return nil
}

func (m *Destination) Sync() error {
	return m.syncer.Sync()
}