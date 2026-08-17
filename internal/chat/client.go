// Package chat streams completions from an OpenAI-compatible chat endpoint —
// ollama's /v1/chat/completions in practice. Sibling of internal/embed: one
// model-touching boundary, no store access, cancellable end to end
// (ATM-66a6d2).
package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"atm/internal/core"
)

const (
	// idleTimeout aborts a stream that has gone quiet. Generous enough to
	// cover a cold ollama model load before the first token.
	//
	// It is deliberately NOT internal/embed's http.Client.Timeout: a
	// whole-request timeout on generation truncates any answer that streams
	// for longer than it, which is a mutilated answer rather than a rescued
	// hang. Silence is the symptom worth acting on here, not duration.
	idleTimeout = 30 * time.Second
	// totalCeiling bounds the whole stream, so a headless `atm ask` inside a
	// script cannot hang forever on an endpoint that dribbles bytes with no
	// human at a keyboard to interrupt it.
	totalCeiling = 5 * time.Minute
)

var (
	// ErrIdleTimeout and ErrCeiling name the two watchdogs, so the answer
	// engine can tell them apart from the caller's own cancellation.
	ErrIdleTimeout = errors.New("chat stream went quiet")
	ErrCeiling     = errors.New("chat stream exceeded its total ceiling")
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	cfg    core.ChatConfig
	client *http.Client
	// idle and total are fields, not constants, so the watchdog tests can run
	// in milliseconds instead of minutes.
	idle  time.Duration
	total time.Duration
}

// New builds a client with NO http.Client.Timeout: see idleTimeout.
func New(cfg core.ChatConfig) *Client {
	return &Client{cfg: cfg, client: &http.Client{}, idle: idleTimeout, total: totalCeiling}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream posts msgs and calls onDelta for every content delta, in order, on
// the calling goroutine. It returns when the endpoint ends the stream, when a
// watchdog kills it, or when ctx is done. Deltas already handed to onDelta
// stay the caller's, whatever the error.
func (c *Client) Stream(ctx context.Context, msgs []Message, onDelta func(string)) error {
	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: msgs, Stream: true})
	if err != nil {
		return err
	}
	// A derived context, so a watchdog abort cancels THIS request without
	// touching the caller's context — which is how the engine can still tell
	// "the user pressed Esc" from "the endpoint went quiet".
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.total > 0 {
		var stopTotal context.CancelFunc
		streamCtx, stopTotal = context.WithTimeout(streamCtx, c.total)
		defer stopTotal()
	}
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.cfg.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return classify(ctx, streamCtx, false, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("chat endpoint %s: status %d: %s", c.cfg.Endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// The idle watchdog starts once headers are in: ollama sends them before
	// it loads the model, so the wait for the first token is on this clock.
	var idled atomic.Bool
	watchdog := time.AfterFunc(c.idle, func() { idled.Store(true); cancel() })
	defer watchdog.Stop()
	r := bufio.NewReader(resp.Body)
	for {
		line, readErr := r.ReadString('\n')
		if payload, ok := ssePayload(line); ok {
			if payload == "[DONE]" {
				return nil
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				// A chunk ATM cannot read is not an answer. Failing loudly
				// beats emitting silence that reads as a finished answer.
				return fmt.Errorf("decode chat chunk: %w", err)
			}
			if chunk.Error != nil {
				return fmt.Errorf("chat error: %s", chunk.Error.Message)
			}
			for _, ch := range chunk.Choices {
				if ch.Delta.Content == "" {
					continue
				}
				watchdog.Reset(c.idle)
				onDelta(ch.Delta.Content)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return classify(ctx, streamCtx, idled.Load(), readErr)
		}
	}
}

// classify names what actually stopped the stream. Order matters: the
// caller's own cancellation outranks the watchdogs, because a canceled ask is
// not a broken endpoint.
func classify(callerCtx, streamCtx context.Context, idled bool, err error) error {
	if callerCtx.Err() != nil {
		return callerCtx.Err()
	}
	if idled {
		return ErrIdleTimeout
	}
	if errors.Is(streamCtx.Err(), context.DeadlineExceeded) {
		return ErrCeiling
	}
	return err
}

// ssePayload extracts one SSE data payload. Keepalive comments (": ping"),
// blank lines, and event: lines carry nothing this client needs.
func ssePayload(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}
