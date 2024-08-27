/*
Copyright 2024 Canonical, Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/canonical/mutating-pebble-webhook-rock/pkg/webhook"
)

func initLogger() {
	if lev := os.Getenv("LOG_LEVEL"); lev != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(lev)); err != nil {
			slog.Error("cannot set LOG_LEVEL.", "level", lev)
			panic(err)
		}
		slog.SetLogLoggerLevel(level)
	}
}

func ensureFile(path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		slog.Error("Expected file to exist, but doesn't.", "path", path)
		panic(err)
	} else if err != nil {
		slog.Error("Encountered error while checking file.", "path", path, "error", err)
		panic(err)
	}
}

func main() {
	initLogger()
	slog.Info("Starting mutating-pebble-webhook...")

	cert := "/etc/admission-webhook/tls/tls.crt"
	key := "/etc/admission-webhook/tls/tls.key"
	ensureFile(cert)
	ensureFile(key)

	tlsCertif, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		slog.Error("Encountered error while loading certificate.", "error", err)
		panic(err)
	}

	server := &http.Server{
		Addr:              ":8443",
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCertif},
			MinVersion:   tls.VersionTLS12,
		},
	}

	http.HandleFunc("/add-pebble-mount", webhook.ServeAddPebbleMount)
	http.HandleFunc("/healthz", webhook.ServeHealthz)

	slog.Info("Listening connections...")
	err = server.ListenAndServeTLS("", "")
	if err != nil {
		slog.Error("Encountered error.", "error", err)
		panic(err)
	}
}
