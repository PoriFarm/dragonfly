package session

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"time"
)

// virtualContainer exists only on the session owner's world goroutine. Fake
// chest blocks are sent to this client alone and never placed in the world.
type virtualContainer struct {
	pos   []cube.Pos
	world *world.World
	token int64
	ready bool
	// acked is set once the client answered the latency request; requested is when the menu was
	// asked for, which bounds the wait for the client to close the previous container.
	acked               bool
	requested           time.Time
	close, afterRequest func(*world.Tx)
}

// virtualContainerFallback opens a menu whose latency answer never came, and maxCloseWait bounds how
// long a menu waits for the client to report the previous container closed.
const (
	virtualContainerFallback = 500 * time.Millisecond
	maxCloseWait             = 1500 * time.Millisecond
)

// openWaitingVirtualContainer opens the pending menu once both the latency answer came and the
// previous container is closed client-side.
func (s *Session) openWaitingVirtualContainer(tx *world.Tx) {
	if menu := s.virtualContainer; menu != nil && !menu.ready && menu.acked && s.closingWindow.Load() == 0 {
		s.openVirtualContainerReady(tx, menu)
	}
}

// fallbackVirtualContainer opens menu after the fallback delay, waiting longer (up to maxCloseWait
// from the request) while the client is still closing the previous container.
func (s *Session) fallbackVirtualContainer(menu *virtualContainer, after time.Duration) {
	time.AfterFunc(after, func() {
		menu.world.Do(func(tx *world.Tx) {
			select {
			case <-s.closeBackground:
				return
			default:
			}
			if s.virtualContainer != menu || menu.ready {
				return
			}
			if s.closingWindow.Load() != 0 && time.Since(menu.requested) < maxCloseWait {
				s.fallbackVirtualContainer(menu, 100*time.Millisecond)
				return
			}
			s.closingWindow.Store(0)
			s.openVirtualContainerReady(tx, menu)
		})
	})
}

func (s *Session) OpenVirtualContainer(tx *world.Tx, pos cube.Pos, title string, inv *inventory.Inventory, close, afterRequest func(*world.Tx)) error {
	if inv.Size() != 27 && inv.Size() != 54 {
		return fmt.Errorf("virtual chest requires 27 or 54 slots")
	}
	s.closeCurrentContainer(tx, false)
	menu := &virtualContainer{pos: []cube.Pos{pos}, world: tx.World(), token: -time.Now().UnixNano(), requested: time.Now(), close: close, afterRequest: afterRequest}
	if inv.Size() == 54 {
		menu.pos = append(menu.pos, pos.Side(cube.FaceEast))
	}
	s.virtualContainer = menu
	s.nextWindowID()
	s.containerOpened.Store(true)
	s.openedWindow.Store(inv)
	s.openedPos.Store(&pos)
	s.openedContainerID.Store(uint32(protocol.ContainerTypeContainer))
	for i, p := range menu.pos {
		chest := block.NewChest()
		chest.Facing, chest.CustomName = cube.North, title
		s.ViewBlockUpdate(p, chest, 0)
		data := chest.EncodeNBT()
		data["x"], data["y"], data["z"] = int32(p.X()), int32(p.Y()), int32(p.Z())
		if len(menu.pos) == 2 {
			pair := menu.pos[1-i]
			data["pairx"], data["pairz"] = int32(pair.X()), int32(pair.Z())
			data["pairlead"] = byte(1 - i)
		}
		s.writePacket(&packet.BlockActorData{Position: protocol.BlockPos{int32(p.X()), int32(p.Y()), int32(p.Z())}, NBTData: data})
	}
	inv.SlotFunc(func(slot int, _, after item.Stack) {
		if s.virtualContainer == menu && menu.ready {
			s.ViewSlotChange(slot, after)
		}
	})
	// Wait for the client to process the fake block before sending ContainerOpen.
	s.writePacket(&packet.NetworkStackLatency{Timestamp: menu.token, NeedsResponse: true})
	// Some proxies consume latency responses. Keep the acknowledgement fast
	// path, but do not leave a menu permanently pending behind such a proxy.
	s.fallbackVirtualContainer(menu, virtualContainerFallback)
	return nil
}

func (s *Session) closeVirtualContainer(tx *world.Tx, clientRequested bool) bool {
	menu := s.virtualContainer
	if menu == nil {
		return false
	}
	s.virtualContainer = nil
	inv := s.openedWindow.Load()
	inv.SlotFunc(func(int, item.Stack, item.Stack) {})
	s.closeWindow(clientRequested)
	if tx.World() == menu.world {
		for _, pos := range menu.pos {
			s.ViewBlockUpdate(pos, tx.Block(pos), 0)
		}
	}
	if menu.close != nil {
		menu.close(tx)
	}
	return true
}

type virtualContainerLatencyHandler struct{}

func (*virtualContainerLatencyHandler) Handle(pk packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	response := pk.(*packet.NetworkStackLatency)
	menu := s.virtualContainer
	if menu == nil || menu.ready || !latencyAnswers(response.Timestamp, menu.token) {
		return nil
	}
	menu.acked = true
	s.openWaitingVirtualContainer(tx)
	return nil
}

// latencyAnswers reports whether a NetworkStackLatency reply answers the request carrying token. The
// 1.26.6x client replies at once but with the timestamp multiplied by 1,000,000 (wrapping, as int64)
// and NeedsResponse still set, so the exact match never held and every menu waited for the 500ms
// fallback (measured 2026-11-18). The plain echo is kept for other clients and proxies.
func latencyAnswers(timestamp, token int64) bool {
	return timestamp == token || timestamp == token*1_000_000
}

func (s *Session) openVirtualContainerReady(tx *world.Tx, menu *virtualContainer) {
	if s.virtualContainer != menu || menu.ready {
		return
	}
	if menu.world != tx.World() {
		s.closeVirtualContainer(tx, false)
		return
	}
	menu.ready = true
	pos := menu.pos[0]
	s.writePacket(&packet.ContainerOpen{WindowID: byte(s.openedWindowID.Load()), ContainerType: protocol.ContainerTypeContainer,
		ContainerPosition: protocol.BlockPos{int32(pos.X()), int32(pos.Y()), int32(pos.Z())}, ContainerEntityUniqueID: -1})
	s.sendInv(s.openedWindow.Load(), s.openedWindowID.Load())
}
