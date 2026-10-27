package whatsapp

import (
	"fmt"
	"sync/atomic"
	"time"

	"watgbridge/state"

	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
)

// backlogCutoff holds the UnixNano time of the latest (re)connection to
// WhatsApp. Events timestamped before it were queued by the server while the
// bridge was offline.
var backlogCutoff atomic.Int64

// markBacklogCutoff records now as the point before which events are
// considered offline backlog. Call it right before connecting and whenever a
// (re)connection is established.
func markBacklogCutoff() {
	backlogCutoff.Store(time.Now().UnixNano())
}

// eventTimestamp returns when a WhatsApp event originally happened, for the
// event types that carry such a timestamp.
func eventTimestamp(evt interface{}) (time.Time, bool) {
	switch v := evt.(type) {
	case *events.Message:
		return v.Info.Timestamp, true
	case *events.UndecryptableMessage:
		return v.Info.Timestamp, true
	case *events.Receipt:
		return v.Timestamp, true
	case *events.CallOffer:
		return v.Timestamp, true
	case *events.Picture:
		return v.Timestamp, true
	case *events.GroupInfo:
		return v.Timestamp, true
	case *events.UserAbout:
		return v.Timestamp, true
	}
	return time.Time{}, false
}

// shouldSkipOfflineBacklog reports whether evt was queued while the bridge was
// offline and should be dropped because skip_offline_backlog is enabled.
func shouldSkipOfflineBacklog(cfg *state.Config, evt interface{}) bool {
	if !cfg.WhatsApp.SkipOfflineBacklog {
		return false
	}

	ts, ok := eventTimestamp(evt)
	if !ok || ts.IsZero() {
		return false
	}

	cutoff := backlogCutoff.Load()
	if cutoff == 0 || !ts.Before(time.Unix(0, cutoff)) {
		return false
	}

	state.State.Logger.Debug("skipping offline backlog event",
		zap.String("event_type", fmt.Sprintf("%T", evt)),
		zap.Time("event_time", ts),
	)
	return true
}
