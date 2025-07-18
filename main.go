package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	serverPort       = ":8080"
	cacheMaxAge      = "max-age=31536000, public"
	defaultMediaType = "application/octet-stream"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	// Validate required environment variables
	assetsApiHost := os.Getenv("ASSETS_API_HOST")
	resizerApiHost := os.Getenv("RESIZER_API_HOST")

	if assetsApiHost == "" {
		log.Fatal("ASSETS_API_HOST environment variable is required")
	}

	if resizerApiHost == "" {
		log.Fatal("RESIZER_API_HOST environment variable is required")
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/assets/*", assetsHandler(assetsApiHost, resizerApiHost))

	srv := &http.Server{
		Addr:    serverPort,
		Handler: r,
	}

	go func() {
		log.Println("Starting server on", serverPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	<-ctx.Done()

	log.Println("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}
	log.Println("Server exiting")
}

func isValidURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func assetsHandler(assetsApiHost, resizerApiHost string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(chi.URLParam(r, "*"), "/")
		if path == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
			return
		}

		urlPath, err := url.QueryUnescape(path)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid path"})
			return
		}

		mediaType := getContentTypeFromFilename(urlPath)

		fullURL := buildFullURL(r, assetsApiHost, resizerApiHost, urlPath)

		resp, err := fetchAsset(fullURL)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Error fetching asset"})
			return
		}
		defer resp.Body.Close()

		setResponseHeaders(w, resp, mediaType)
		io.Copy(w, resp.Body)
	}
}

func buildFullURL(r *http.Request, assetsApiHost, resizerApiHost, urlPath string) string {
	if !isValidURL(urlPath) {
		urlPath = fmt.Sprintf("%s/assets/%s", assetsApiHost, urlPath)
	}

	contentType := r.URL.Query().Get("type")

	if contentType == "image" {
		width := r.URL.Query().Get("w")
		height := r.URL.Query().Get("h")
		u, _ := url.Parse(resizerApiHost)
		var comp string
		if width != "" && height == "" {
			comp = fmt.Sprintf("w:%s", width)
		}
		if height != "" && width == "" {
			comp = fmt.Sprintf("h:%s", height)
		}
		if width != "" && height != "" {
			comp = fmt.Sprintf("w:%s/h:%s", width, height)
		}
		if comp != "" {
			u.Path = fmt.Sprintf("/insecure/%s/plain/%s", comp, urlPath)
		} else {
			u.Path = fmt.Sprintf("/insecure/plain/%s", urlPath)
		}
		return u.String()
	}

	return urlPath
}

func fetchAsset(fullURL string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fullURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return resp, nil
}

func setResponseHeaders(w http.ResponseWriter, resp *http.Response, mediaType string) {
	if contentDisposition := resp.Header.Get("Content-Disposition"); contentDisposition != "" {
		w.Header().Set("Content-Disposition", contentDisposition)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mediaType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
	w.Header().Set("Cache-Control", cacheMaxAge)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
}

func getContentTypeFromFilename(urlPath string) string {
	_, filename := filepath.Split(urlPath)

	filename = strings.Split(filename, "?")[0]

	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	if mimeType == "" {
		mimeType = defaultMediaType
	}

	return mimeType
}
