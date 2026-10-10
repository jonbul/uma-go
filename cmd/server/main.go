// Command server runs the UMA online lobby.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uma-go/internal/lobby"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	minVersion := flag.String("min-version", "0.1.0", "oldest game version allowed to play")
	latestVersion := flag.String("latest-version", "", "newest game version; older ones are told to update (default: min-version)")
	androidURL := flag.String("android-url", "", "where Android players get updates (store page)")
	iosURL := flag.String("ios-url", "", "where iOS players get updates (store page)")
	webURL := flag.String("web-url", "", "where web players get updates (default: they reload the page)")
	flag.Parse()

	policy := lobby.VersionPolicy{MinVersion: *minVersion, LatestVersion: *latestVersion, UpdateURLs: map[string]string{}}
	if policy.LatestVersion == "" {
		policy.LatestVersion = policy.MinVersion
	}
	for platform, url := range map[string]string{"android": *androidURL, "ios": *iosURL, "web": *webURL} {
		if url != "" {
			policy.UpdateURLs[platform] = url
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", lobby.NewHub(policy))
	mux.Handle("GET /version", policy)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: *addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("listening", "addr", *addr, "min_version", policy.MinVersion, "latest_version", policy.LatestVersion)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
