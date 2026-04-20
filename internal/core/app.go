package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/config"
	"github.com/shiroonigami23-ui/shiro-distributed-system/internal/modules"
)

type App struct {
	cfg     config.Config
	modules []modules.Module
}

func New(cfg config.Config, ms ...modules.Module) *App {
	return &App{cfg: cfg, modules: ms}
}

func (a *App) Run(ctx context.Context) error {
	for _, m := range a.modules {
		if err := m.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", m.Name(), err)
		}
		log.Printf("module started: %s", m.Name())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		tctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for _, m := range a.modules {
			if err := m.Ready(tctx); err != nil {
				http.Error(w, m.Name()+": "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: a.cfg.HTTPAddr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("control plane listening on %s", a.cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	for i := len(a.modules) - 1; i >= 0; i-- {
		m := a.modules[i]
		if err := m.Stop(shutdownCtx); err != nil {
			log.Printf("module stop warning [%s]: %v", m.Name(), err)
		}
	}
	return nil
}
