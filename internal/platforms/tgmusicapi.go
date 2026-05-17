/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package platforms

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	state "main/internal/core/models"
)

const PlatformTgMusicAPI state.PlatformName = "TgMusicAPI"

// tgMusicAPIResponse mirrors the JSON returned by the @tgmusic_apibot proxy.
// Example:
//
//	{ "status": "success", "audio_url": "https://...", "video_url": "https://..." }
type tgMusicAPIResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`   // populated on error
	AudioURL string `json:"audio_url"` // used for audio downloads
	VideoURL string `json:"video_url"` // used for video downloads
}

// TgMusicAPIPlatform is a download-only platform that fetches pre-encoded
// audio/video from the TgMusic proxy API (set via YT_API_KEY + YTPROXY_URL).
// It handles YouTube tracks and runs at priority 85, sitting between
// FallenAPI (80) and YouTube (90) so it is tried before the slower yt-dlp fallback.
type TgMusicAPIPlatform struct {
	name state.PlatformName
}

func init() {
	Register(85, &TgMusicAPIPlatform{
		name: PlatformTgMusicAPI,
	})
}

func (t *TgMusicAPIPlatform) Name() state.PlatformName {
	return t.name
}

// CanGetTracks — this is a download-only platform, no track resolution here.
func (t *TgMusicAPIPlatform) CanGetTracks(_ string) bool {
	return false
}

// GetTracks — not supported; download-only.
func (t *TgMusicAPIPlatform) GetTracks(_ string, _ bool) ([]*state.Track, error) {
	return nil, errors.New("tgmusicapi is a download-only platform")
}

// CanDownload returns true only when both API credentials are configured
// and the track source is YouTube (this API is a YT-specific proxy).
func (t *TgMusicAPIPlatform) CanDownload(source state.PlatformName) bool {
	if config.YTAPIKey == "" || config.YTProxyURL == "" {
		return false
	}
	return source == PlatformYouTube
}

// Download fetches the track from the TgMusic proxy and streams it to disk.
// Audio downloads use audio_url; video downloads use video_url.
// On any failure the caller (registry.Download) will transparently fall through
// to the next capable downloader (e.g. YtDlp).
func (t *TgMusicAPIPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {
	// Re-use a cached file if one already exists.
	if f := findFile(track); f != "" {
		gologging.Debug("TgMusicAPI: Download -> Cached File -> " + f)
		return f, nil
	}

	gologging.InfoF("TgMusicAPI: Downloading %q (video=%v)", track.Title, track.Video)

	// --- 1. Fetch metadata from the proxy ----------------------------------
	apiURL := fmt.Sprintf("%s/info/%s", config.YTProxyURL, track.ID)

	var apiResp tgMusicAPIResponse
	resp, err := rc.R().
		SetContext(ctx).
		SetHeader("x-api-key", config.YTAPIKey).
		SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36").
		SetResult(&apiResp).
		Get(apiURL)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf(
			"TgMusicAPI: info request failed: %w",
			sanitizeAPIError(err, config.YTAPIKey),
		)
	}

	if resp.IsError() {
		return "", fmt.Errorf(
			"TgMusicAPI: info request returned HTTP %d: %s",
			resp.StatusCode(), resp.String(),
		)
	}

	if apiResp.Status != "success" {
		msg := apiResp.Message
		if msg == "" {
			msg = "unknown error from API"
		}
		return "", fmt.Errorf("TgMusicAPI: API error: %s", msg)
	}

	// --- 2. Pick the correct CDN URL ---------------------------------------
	var cdnURL string
	var ext string
	if track.Video {
		cdnURL = apiResp.VideoURL
		ext = ".mp4"
	} else {
		cdnURL = apiResp.AudioURL
		ext = ".mp3"
	}

	if cdnURL == "" {
		return "", fmt.Errorf(
			"TgMusicAPI: API returned success but %s URL is empty",
			map[bool]string{true: "video", false: "audio"}[track.Video],
		)
	}

	// --- 3. Stream the file to disk ----------------------------------------
	path := getPath(track, ext)

	dlResp, err := rc.R().
		SetContext(ctx).
		SetHeader("x-api-key", config.YTAPIKey).
		SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36").
		SetOutputFileName(path).
		Get(cdnURL)

	if err != nil {
		os.Remove(path)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf("TgMusicAPI: download failed: %w", err)
	}

	if dlResp.IsError() {
		os.Remove(path)
		return "", fmt.Errorf(
			"TgMusicAPI: download returned HTTP %d",
			dlResp.StatusCode(),
		)
	}

	if !fileExists(path) {
		return "", errors.New("TgMusicAPI: downloaded file is empty or missing")
	}

	gologging.InfoF("TgMusicAPI: Successfully downloaded -> %s", path)
	return path, nil
}

func (*TgMusicAPIPlatform) CanSearch() bool { return false }

func (*TgMusicAPIPlatform) Search(_ string, _ bool) ([]*state.Track, error) {
	return nil, nil
}
