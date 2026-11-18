package session

import (
	"context"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"testing"
	"time"
)

func TestVirtualChestOpensWithoutLatencyResponse(t *testing.T) {
	w := world.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
	if err := w.Do(func(tx *world.Tx) {
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "fallback", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case p := <-s.packets:
			if _, ok := p.(*packet.ContainerOpen); ok {
				return
			}
		case <-timer.C:
			t.Fatal("menu remained pending without latency response")
		}
	}
}

func TestVirtualChestAcknowledgementAndClose(t *testing.T) {
	w := world.New()
	defer w.Close()
	err := w.Do(func(tx *world.Tx) {
		for _, size := range []int{27, 54} {
			s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
			pos := cube.Pos{0, 64, 0}
			tx.SetBlock(pos, block.Stone{}, nil)
			inv := inventory.New(size, nil)
			_ = inv.SetItem(10, item.NewStack(item.Diamond{}, 3))
			closed := 0
			if err := s.OpenVirtualContainer(tx, pos, "§c§1§btitle", inv, func(*world.Tx) { closed++ }, nil); err != nil {
				t.Fatal(err)
			}
			if _, ok := tx.Block(pos).(block.Stone); !ok {
				t.Fatal("fake chest changed world")
			}
			if _, ok := s.invByID(protocol.ContainerLevelEntity, tx); ok {
				t.Fatal("inventory accessible before acknowledgement")
			}
			h := &virtualContainerLatencyHandler{}
			_ = h.Handle(&packet.NetworkStackLatency{Timestamp: s.virtualContainer.token + 1}, s, tx, nil)
			if s.virtualContainer.ready {
				t.Fatal("stale acknowledgement opened menu")
			}
			_ = h.Handle(&packet.NetworkStackLatency{Timestamp: s.virtualContainer.token}, s, tx, nil)
			if i, ok := s.invByID(protocol.ContainerLevelEntity, tx); !ok || i != inv {
				t.Fatal("menu not accessible")
			}
			if _, ok := s.invByID(protocol.ContainerFurnaceIngredient, tx); ok {
				t.Fatal("fake container exposed unrelated block inventory")
			}
			opens := 0
			for len(s.packets) > 0 {
				if _, ok := (<-s.packets).(*packet.ContainerOpen); ok {
					opens++
				}
			}
			if opens != 1 {
				t.Fatal("expected exactly one open", opens)
			}
			s.CloseContainer(tx)
			s.CloseContainer(tx)
			if closed != 1 || s.virtualContainer != nil || s.containerOpened.Load() {
				t.Fatal("close must be idempotent")
			}
		}
	}).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// TestVirtualChestOpensOnClientScaledLatency checks the reply of the 1.26.6x client: the timestamp
// multiplied by 1,000,000 with NeedsResponse still set opens the menu at once, not after the fallback.
func TestVirtualChestOpensOnClientScaledLatency(t *testing.T) {
	w := world.New()
	defer w.Close()
	err := w.Do(func(tx *world.Tx) {
		s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "scaled", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		h := &virtualContainerLatencyHandler{}
		token := s.virtualContainer.token
		_ = h.Handle(&packet.NetworkStackLatency{Timestamp: (token + 1) * 1_000_000, NeedsResponse: true}, s, tx, nil)
		if s.virtualContainer.ready {
			t.Fatal("stale scaled acknowledgement opened menu")
		}
		_ = h.Handle(&packet.NetworkStackLatency{Timestamp: token * 1_000_000, NeedsResponse: true}, s, tx, nil)
		if !s.virtualContainer.ready {
			t.Fatal("client's scaled acknowledgement did not open menu")
		}
	}).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The captured 1.26.62 reply to a real token.
	if !latencyAnswers(-4116760413260901888, -1795001442340868600) {
		t.Fatal("captured client reply not recognised")
	}
}

func TestVirtualChestFallbackDoesNotReopenClosedMenu(t *testing.T) {
	w := world.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
	if err := w.Do(func(tx *world.Tx) {
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "closed", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		s.CloseContainer(tx)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.DoAfter(time.Second, func(*world.Tx) {}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	for len(s.packets) > 0 {
		if _, ok := (<-s.packets).(*packet.ContainerOpen); ok {
			t.Fatal("fallback reopened closed menu")
		}
	}
}

func TestRequestRollbackKeepsFirstSlotSnapshot(t *testing.T) {
	w := world.New()
	defer w.Close()
	err := w.Do(func(tx *world.Tx) {
		s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{}), inv: inventory.New(36, nil)}
		initial := item.NewStack(item.Diamond{}, 9)
		_ = s.inv.SetItem(0, initial)
		h := &ItemStackRequestHandler{currentRequest: 1, changes: map[byte]map[byte]changeInfo{}, responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{}}
		slot := protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerInventory}, Slot: 0}
		h.setItemInSlot(slot, initial.Grow(-1), s, tx)
		h.setItemInSlot(slot, initial.Grow(-3), s, tx)
		h.reject(1, s, tx)
		got, _ := s.inv.Item(0)
		if !got.Equal(initial) {
			t.Fatal("rejected multi-action request did not restore original stack", got)
		}
	}).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// TestVirtualChestWaitsForClientClose checks a menu opened right after the server closed another:
// the latency answer alone does not open it while the client is still closing the old container
// (it would close the new one with ContainerClose 255); the client's close report does, and with
// no report the menu still opens within maxCloseWait.
func TestVirtualChestWaitsForClientClose(t *testing.T) {
	w := world.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
	if err := w.Do(func(tx *world.Tx) {
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "first", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		h := &virtualContainerLatencyHandler{}
		_ = h.Handle(&packet.NetworkStackLatency{Timestamp: s.virtualContainer.token * 1_000_000, NeedsResponse: true}, s, tx, nil)
		old := byte(s.openedWindowID.Load())
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "second", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		if s.closingWindow.Load() != uint32(old)+1 {
			t.Fatal("server close not remembered")
		}
		_ = h.Handle(&packet.NetworkStackLatency{Timestamp: s.virtualContainer.token * 1_000_000, NeedsResponse: true}, s, tx, nil)
		if s.virtualContainer.ready {
			t.Fatal("menu opened while the client was still closing the previous one")
		}
		if !s.closingWindow.CompareAndSwap(uint32(old)+1, 0) {
			t.Fatal("close report not matched")
		}
		s.openWaitingVirtualContainer(tx)
		if !s.virtualContainer.ready {
			t.Fatal("client's close report did not open the waiting menu")
		}
		// No close report at all: the fallback still opens the next menu in time.
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "third", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(maxCloseWait + time.Second)
	for time.Now().Before(deadline) {
		ready := false
		_ = w.Do(func(*world.Tx) { ready = s.virtualContainer != nil && s.virtualContainer.ready }).Wait(context.Background())
		if ready {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("menu stayed pending without a close report")
}

// A menu the client refuses with ContainerClose(255), as it does while the chat a command was typed
// in is still closing, is sent again after a latency round trip instead of being lost, at most
// maxRefusedOpens times.
func TestVirtualChestRetriesRefusedOpen(t *testing.T) {
	w := world.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
	opens := func() int {
		n := 0
		for {
			select {
			case p := <-s.packets:
				if _, ok := p.(*packet.ContainerOpen); ok {
					n++
				}
			default:
				return n
			}
		}
	}
	do := func(f func(tx *world.Tx)) {
		if err := w.Do(f).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	do(func(tx *world.Tx) {
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "refused", inventory.New(54, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		s.virtualContainer.acked = true
		s.openWaitingVirtualContainer(tx)
	})
	if n := opens(); n != 1 {
		t.Fatalf("first open sent %d ContainerOpen", n)
	}
	for i := 0; i < maxRefusedOpens; i++ {
		do(func(tx *world.Tx) {
			if !s.retryRefusedContainer() {
				t.Fatalf("refusal %d not retried", i+1)
			}
			if s.virtualContainer.ready {
				t.Fatal("refused menu sent again before the latency answer")
			}
			// The client answers the new latency request once it has handled the close.
			h := &virtualContainerLatencyHandler{}
			_ = h.Handle(&packet.NetworkStackLatency{Timestamp: s.virtualContainer.token * 1_000_000, NeedsResponse: true}, s, tx, nil)
		})
		if n := opens(); n != 1 {
			t.Fatalf("retry %d sent %d ContainerOpen", i+1, n)
		}
	}
	do(func(*world.Tx) {
		if s.retryRefusedContainer() {
			t.Fatal("refused menu retried past maxRefusedOpens")
		}
	})
}

// Closing a menu the client refused does not leave the next menu waiting for a close report the
// client never sends.
func TestRefusedMenuCloseDoesNotWait(t *testing.T) {
	w := world.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 100), closeBackground: make(chan struct{})}
	if err := w.Do(func(tx *world.Tx) {
		if err := s.OpenVirtualContainer(tx, cube.Pos{0, 64, 0}, "refused", inventory.New(27, nil), nil, nil); err != nil {
			t.Fatal(err)
		}
		s.closeCurrentContainer(tx, true)
		if s.closingWindow.Load() != 0 {
			t.Fatal("refused menu left a close wait")
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
