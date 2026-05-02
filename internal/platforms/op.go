package platforms

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"

	state "main/internal/core/models"
)

const PlatformYTProxy state.PlatformName = "YTProxy"

type ytProxyInfoResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	AudioURL string `json:"audio_url"`
	VideoURL string `json:"video_url"`
}

type YTProxyPlatform struct {
	name state.PlatformName
}

func init() {
	Register(85, &YTProxyPlatform{name: PlatformYTProxy})
}

func (p *YTProxyPlatform) Name() state.PlatformName { return p.name }

func (p *YTProxyPlatform) CanSearch() bool { return false }

func (p *YTProxyPlatform) Search(_ string, _ bool) ([]*state.Track, error) {
	return nil, errors.New("ytproxy is a download-only platform")
}

func (p *YTProxyPlatform) CanGetTracks(_ string) bool { return false }

func (p *YTProxyPlatform) GetTracks(_ string, _ bool) ([]*state.Track, error) {
	return nil, errors.New("ytproxy is a download-only platform")
}

func (p *YTProxyPlatform) CanDownload(source state.PlatformName) bool {
	apiKey := strings.TrimSpace(os.Getenv("YT_API_KEY"))
	proxyURL := strings.TrimSpace(os.Getenv("YTPROXY_URL"))
	if apiKey == "" || proxyURL == "" {
		return false
	}
	return source == PlatformYouTube
}

func (p *YTProxyPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {
	if f := findFile(track); f != "" {
		return f, nil
	}

	apiKey := strings.TrimSpace(os.Getenv("YT_API_KEY"))
	proxyURL := strings.TrimRight(strings.TrimSpace(os.Getenv("YTPROXY_URL")), "/")
	if apiKey == "" {
		return "", errors.New("YT_API_KEY is missing")
	}
	if proxyURL == "" {
		return "", errors.New("YTPROXY_URL is missing")
	}

	path := getPath(track, ".mp3")
	mediaField := "audio_url"
	if track.Video {
		path = getPath(track, ".mp4")
		mediaField = "video_url"
	}

	var info ytProxyInfoResponse
	resp, err := rc.R().
		SetContext(ctx).
		SetHeader("x-api-key", apiKey).
		SetResult(&info).
		Get(fmt.Sprintf("%s/info/%s", proxyURL, track.ID))
	if err != nil {
		return "", sanitizeAPIError(err, apiKey)
	}
	if resp.IsError() {
		return "", fmt.Errorf("ytproxy info request failed: status %d", resp.StatusCode())
	}
	if info.Status != "success" {
		if info.Message == "" {
			info.Message = "unknown API error"
		}
		return "", fmt.Errorf("ytproxy API error: %s", info.Message)
	}

	dlURL := info.AudioURL
	if mediaField == "video_url" {
		dlURL = info.VideoURL
	}
	if dlURL == "" {
		return "", fmt.Errorf("ytproxy returned empty %s", mediaField)
	}

	dlResp, err := rc.R().
		SetContext(ctx).
		SetHeader("x-api-key", apiKey).
		SetOutputFileName(path).
		Get(dlURL)
	if err != nil {
		_ = os.Remove(path)
		return "", sanitizeAPIError(err, apiKey)
	}
	if dlResp.IsError() {
		_ = os.Remove(path)
		return "", fmt.Errorf("ytproxy media download failed: status %d", dlResp.StatusCode())
	}
	if !fileExists(path) {
		_ = os.Remove(path)
		return "", errors.New("ytproxy returned empty file")
	}
	return path, nil
}
