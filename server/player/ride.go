package player

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// linkViewer is a world.Viewer that can show one entity riding another. The network session
// implements it; other viewers simply do not see the link.
type linkViewer interface {
	ViewEntityLink(rider, ridden world.Entity, linked bool)
}

// Mount seats the player on e, with the seat seat blocks away from e's position. The client
// draws the player sitting there until Dismount is called, the player sneaks, leaves the vehicle,
// teleports or dies. Mounting another entity first leaves the current one.
func (p *Player) Mount(e world.Entity, seat mgl64.Vec3) {
	if e == nil || e.H() == p.handle {
		return
	}
	if p.mount != nil {
		p.Dismount()
	}
	p.mount, p.seat = e.H(), seat
	p.teleport(e.Position().Add(seat))
	p.updateState()
	for _, v := range p.viewers() {
		if lv, ok := v.(linkViewer); ok {
			lv.ViewEntityLink(p, e, true)
		}
	}
}

// Dismount makes the player leave the entity it sits on, if any, and stand just above the seat.
func (p *Player) Dismount() {
	h := p.mount
	if h == nil {
		return
	}
	p.mount, p.seat = nil, mgl64.Vec3{}
	ridden, ok := h.Entity(p.tx)
	for _, v := range p.viewers() {
		if lv, ok2 := v.(linkViewer); ok2 && ok {
			lv.ViewEntityLink(p, ridden, false)
		}
	}
	p.updateState()
	if ok {
		p.teleport(ridden.Position().Add(mgl64.Vec3{0, 1, 0}))
	}
}

// Mounted returns the entity the player sits on.
func (p *Player) Mounted() (*world.EntityHandle, bool) {
	return p.mount, p.mount != nil
}

// SeatOffset returns where the player sits relative to the entity it rides, and false when it
// rides nothing. The network session puts it in the player's metadata.
func (p *Player) SeatOffset() (mgl64.Vec3, bool) {
	return p.seat, p.mount != nil
}

// SetRideInput records the movement keys the client holds while riding: move is its move vector
// (x sideways, y forward, each -1 to 1) and jump whether jump is held. The network session sets it
// with every input packet; it is zero while the player rides nothing.
func (p *Player) SetRideInput(move mgl64.Vec2, jump bool) {
	if p.mount == nil {
		move, jump = mgl64.Vec2{}, false
	}
	p.rideMove, p.rideJump = move, jump
}

// RideInput returns the movement keys the player holds while riding, so the entity it rides can be
// steered by it (a vehicle the server moves). Both are zero while the player rides nothing.
func (p *Player) RideInput() (move mgl64.Vec2, jump bool) {
	if p.mount == nil {
		return mgl64.Vec2{}, false
	}
	return p.rideMove, p.rideJump
}

// RequestDismount handles the client asking to get off the entity the player rides. The client
// also asks when jump is pressed on a vehicle it does not steer itself, while the vehicles the
// server moves take jump as an input (RideInput), so a request made while jumping keeps the player
// seated and shows the seat again to the client (which already left it on its side) and to every
// viewer. Otherwise the player gets off; sneaking gets off as well (StartSneaking).
func (p *Player) RequestDismount(jumping bool) {
	if p.mount == nil {
		return
	}
	if !jumping {
		p.Dismount()
		return
	}
	ridden, ok := p.mount.Entity(p.tx)
	if !ok {
		p.Dismount()
		return
	}
	for _, v := range p.viewers() {
		if lv, ok := v.(linkViewer); ok {
			lv.ViewEntityLink(p, ridden, true)
		}
	}
}
