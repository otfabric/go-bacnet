// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"log/slog"
	"sort"

	"github.com/otfabric/go-bacnet/internal/diag"
)

// newDiagSink combines the optional logger and diagnostic callback into the
// sink the client reports to. With neither, diagnostics are discarded.
func newDiagSink(l *slog.Logger, fn func(Diagnostic)) diag.Sink {
	if l == nil && fn == nil {
		return diag.Discard{}
	}
	return diag.Func(func(e diag.Event) {
		if fn != nil {
			fn(Diagnostic{Kind: string(e.Kind), Message: e.Message, Fields: e.Fields})
		}
		if l != nil {
			logDiag(l, e)
		}
	})
}

// diagLevel is the log level of a diagnostic kind.
func diagLevel(k diag.Kind) slog.Level {
	switch k {
	case diag.KindTransport, diag.KindForeignDevice, diag.KindQueueDrop,
		diag.KindRegistryEviction, diag.KindDuplicateInstance:
		return slog.LevelWarn
	case diag.KindRouter:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

func logDiag(l *slog.Logger, e diag.Event) {
	level := diagLevel(e.Kind)
	ctx := context.Background()
	if !l.Enabled(ctx, level) {
		return
	}
	attrs := make([]slog.Attr, 0, len(e.Fields)+1)
	attrs = append(attrs, slog.String("kind", string(e.Kind)))
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		attrs = append(attrs, slog.Any(k, e.Fields[k]))
	}
	l.LogAttrs(ctx, level, e.Message, attrs...)
}
