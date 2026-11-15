package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global setting keys stored in the existing settings key-value table.
const (
	// settingNotifyEnabled is the master delivery switch, enabled by default.
	// It allows maintenance shutdown; configuring a channel enables the feature.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external URL used for finding detail links
	// (for example, https://artex.example.com). Empty values omit the link button.
	// The project has no existing external-address setting to reuse.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest interval in minutes.
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery polling interval. Three seconds determines
	// real-time responsiveness and most of the delay from persistence to IM delivery.
	notifyTick = 3 * time.Second
	// notifyBackgroundDisabledEnv prevents startup of newNotifier(s).Run.
	// Integration tests call stepRealtime and stepDigest directly for deterministic
	// results; a concurrent three-second background loop would race on the same
	// channels. This test-only switch leaves normal production startup unchanged.
	notifyBackgroundDisabledEnv = "ARTEX_NOTIFY_BACKGROUND_DISABLED"
	// notifyLease is the delivery claim lease. It must greatly exceed the worst-case
	// send time (the notify HTTP client times out after 15 seconds), preventing
	// two dispatchers from delivering the same row concurrently.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick caps event fan-out per tick so enabling a channel does not
	// turn the entire historical backlog into delivery tasks at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick caps each tick when a channel has no rate limit.
	// This prevents thousands of new findings from blocking one tick for too long.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick caps sends per channel per tick.
	//
	// The cap is derived from notifyLease (three minutes). Too many serial sends
	// could outlast the lease before the last send completes. Run is serial within
	// one process, but another process sharing the DB could reclaim expired rows,
	// send duplicates, increment attempts twice, and mark in-flight sends failed.
	//
	// Three minutes / 30 seconds = six sends leaves no margin. Five sends take at
	// most 150 seconds, leaving 30 seconds spare. TestNotifyTickBudgetFitsWithinLease
	// pins this relationship among notifyLease, notifySendTimeout, and this cap.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout is the per-send timeout. Multiplied by the cap above, it must
	// fit within notifyLease; see TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is indexed by the number of attempts already made. Its three
// attempts, including the initial send, must stay aligned with db.MaxNotifyAttempts.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Delivery failure and delay reasons stored in notification_deliveries.last_error
// and returned through notify_api.go to the delivery history table. These are
// user-facing English messages.
const (
	errDeliveryChannelKindUnregistered = "Unregistered channel type: %q"
	errDeliverySnapshotUnrenderable    = "Could not parse the event snapshot, so this vulnerability could not be turned into a message"
	errDeliveryChannelLengthCapped     = "This message reached the channel length limit, so only the first %d were sent and the rest are deferred to the next batch"
	errDeliveryNoDeliveredCount        = "The channel did not report the number of messages sent (delivered=%d)"
	errDeliveryRetryExhausted          = "Still failed after %d retries: %s"
	errDeliverySnapshotEmpty           = "The event snapshot for delivery item #%d is empty"
	errDeliverySnapshotParse           = "Failed to parse the event snapshot of delivery item #%d: %w"
	errDeliveryBatchAllUnparseable     = "Failed to parse all %d deliveries in the summary batch"
)

// Notifier is the finding notification delivery engine.
//
// It runs alongside Scheduler in a separate goroutine (see server.New). Delivery
// needs a three-second cadence and independent failures: stalled notifications
// must not prevent agent triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects buckets. Few channels and low contention make one mutex sufficient.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is the token bucket for one channel.
//
// A token bucket avoids fixed-window boundary bursts: sending 20 messages at
// one minute's end and 20 at the next one's start would appear as 40 in a second.
// Constant-rate replenishment avoids that burst and the resulting rate limiting.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. server.New starts it once.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step fans out new events, then sends due deliveries.
//
// Failures are logged without stopping the loop or affecting the process.
// Each tick is independent, so the next one naturally retries.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to dispatch events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to read channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// Tokens count messages (HTTP requests), not findings. Real-time mode sends
		// one message per finding; digest mode combines the batch into one message
		// and consumes only one token.
		//
		// Obtain tokens before claiming deliveries in both modes so rate-limited
		// deliveries do not consume retry attempts.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns the digest token cost and maximum batch size for this tick.
//
// The two return values use different units:
//   - tokens counts messages. A digest is one HTTP request, so this is always one.
//     rate_per_min therefore still limits the number of digest messages per minute.
//   - claimLimit counts findings per batch and depends on memory, not request budget.
//
// Previously the lease-derived request budget was used as the batch size. At
// rate_per_min=20, a three-second tick replenishes only one token, making each
// digest contain one finding and preventing db.MaxDigestBatchSize from being used.
// Existing end-to-end tests passed large limits directly to stepDigest, bypassing
// this calculation. TestDigestTickPlanDecouplesBatchSizeFromSendBudget pins it.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and sends one message per finding for a channel.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim realtime deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// Rendering failures are local data problems; retries will not fix them.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates due deliveries for a channel into one digest message.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to evaluate digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Explicitly fail deliveries with invalid snapshots that could not enter the
	// message. Otherwise batch marking omits them and they remain sending until
	// their leases expire, causing repeated claims.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := errDeliverySnapshotUnrenderable
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark deliveries with bad snapshots channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparsable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Send only included deliveries: included[i] must match msg.Items[i]. send uses
	// that mapping to mark the first K items reported as delivered by the channel.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers the message and updates delivery states from the result.
//
// All deliveries in a batch share one send result: delivered or retried together.
// Retrying individual digest items would break the single-message batch semantics.
//
// Channel length limits are the exception: if only the first K items fit, items
// K+1 onward must wait for the next batch. Marking them successful would silently
// lose findings that appear in neither the message nor the failure list.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Bound each send so a stalled channel cannot block all remaining channels.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// A count exceeding the deliveries indicates a rendering bug. Treat all as
			// delivered and log the problem instead of corrupting delivery records.
			log.Printf("[notify] channel reported %d delivered, more than the %d sent channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark as delivered channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// The message reached the channel length limit. Requeue the remainder for
			// the next tick with DeferDeliveries, not RescheduleDeliveries: this is
			// not a failure, so undo the optimistic attempt increment from claiming.
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf(trMsg(errDeliveryChannelLengthCapped), delivered)); err != nil {
				log.Printf("[notify] failed to queue remaining segments channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel returned neither an error nor a delivered count. Fail with
		// backoff to prevent endless claims without a final status.
		err = fmt.Errorf(trMsg(errDeliveryNoDeliveredCount), delivered)
	}

	// Decide failure handling per delivery, not by the batch's maximum attempts.
	// Previously maxAttempts(deliveries) >= MaxNotifyAttempts failed an entire
	// batch. An older delivery could therefore exhaust a fresh delivery's budget
	// before it had any retries, permanently losing a new finding.
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf(trMsg(errDeliveryRetryExhausted), db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Group rescheduling by delay. Only three backoff levels exist, avoiding one
	// UPDATE per delivery and hundreds of round trips for a large batch.
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to requeue deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d retries_exhausted=%d pending_retry=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns entries in all absent from keep, using pointer identity.
// These omitted deliveries require explicit handling rather than being left pending.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt retrieves the channel implementation and parses its configuration.
// An unregistered type returns ok=false and should fail without endless retries.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// Use an empty map if parsing fails. The channel's Validate reports missing
		// fields more helpfully than a raw JSON parse error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders a message for one finding.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch renders a digest, parsing snapshots individually so one invalid
// snapshot does not discard the entire batch.
//
// included[i] must correspond exactly to msg.Items[i]. The caller marks the first
// K deliveries sent when the channel reports that K items fit. An invalid snapshot
// must therefore be omitted from both slices; otherwise a failed item could be
// marked delivered and a valid item left unsent. stepDigest explicitly fails the
// omitted deliveries.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// Invalid snapshots enter neither the message nor included. The caller
			// explicitly fails them instead of mistakenly marking them delivered.
			log.Printf("[notify] skipped unparsable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf(trMsg(errDeliveryBatchAllUnparseable), len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders a snapshot and resolves its asset name and finding detail link.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Asset lookup failure must not prevent delivery: omitting an asset name
		// is preferable to losing the notification.
		log.Printf("[notify] failed to resolve asset name finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The detail route at web/src/app/(main)/function/findings/detail/page.tsx
		// reads the finding ID from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens removes at most want tokens from the channel bucket and returns the count.
//
// One token is one message (one HTTP request). Real-time mode requests the needed
// message count; digest mode requests one token for the entire batch.
//
// Capacity equals the per-minute channel limit, with constant replenishment.
// ratePerMin<=0 returns a large finite allowance to avoid unbounded tick processing.
//
// The want cap prevents draining tokens the caller's own per-tick limit cannot
// use. Without it, accumulated burst capacity disappears before replenishment,
// even on ticks with no pending deliveries.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Replenish by elapsed wall time at ratePerMin/60 tokens per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before truncating. Floating-point accumulation could
	// produce 0.9999999999 instead of 1 after two half-token refills. 1e-9 corrects
	// that rounding error without permitting a genuine token deficit.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external link base without a trailing slash.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the configured interval, falling back on invalid or missing values.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot parses the event snapshot for a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotEmpty), dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotParse), dl.ID, err)
	}
	if snap.Kind == "" {
		// The event row is authoritative; the snapshot may come from an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
