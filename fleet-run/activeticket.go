package main

// tmux user options tracking which ticket's windows are currently live.
// Since window names no longer encode the ticket (see window.go), this is
// the only record of "what's running belongs to which ticket" -- and, per
// the contract, it doubles as something the user's own tmux config can
// reference in a status-line format string (e.g.
// "#{@fleet_task_ticket}: #{@fleet_task_description}").
//
// These are set with global (-g) scope rather than tied to the fleet
// session specifically -- see setSessionOptionArgs in tmux_cmd.go for why
// that's necessary, not just simpler. Since fleet-run only ever runs one
// session, this is equivalent in practice to a true per-session value.
const (
	activeTicketOption      = "@fleet_task_ticket"
	activeDescriptionOption = "@fleet_task_description"
)

// activeTicket returns the ticket currently recorded as active, or
// found=false if none is set (fresh session, or everything's been stopped).
func activeTicket() (ticket string, found bool, err error) {
	return getSessionOption(activeTicketOption)
}

// setActiveTicket records ts as the active ticket.
func setActiveTicket(ts *TaskState) error {
	if err := setSessionOption(activeTicketOption, ts.Ticket); err != nil {
		return err
	}
	return setSessionOption(activeDescriptionOption, ts.Description)
}

// clearActiveTicket removes the active-ticket record, for when every window
// in the session has been stopped and nothing is running anymore.
func clearActiveTicket() error {
	if err := unsetSessionOption(activeTicketOption); err != nil {
		return err
	}
	return unsetSessionOption(activeDescriptionOption)
}
