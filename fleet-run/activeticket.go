package main

// Session-scoped tmux user options tracking which ticket's windows are
// currently live. Since window names no longer encode the ticket (see
// window.go), this is the only record of "what's running belongs to which
// ticket" -- and, per the contract, it doubles as something the user's own
// tmux config can reference in a status-line format string (e.g.
// "#{@fleet_task_ticket}: #{@fleet_task_description}").
const (
	activeTicketOption      = "@fleet_task_ticket"
	activeDescriptionOption = "@fleet_task_description"
)

// activeTicket returns the ticket currently recorded as active for session,
// or found=false if none is set (fresh session, or everything's been
// stopped).
func activeTicket(session string) (ticket string, found bool, err error) {
	return getSessionOption(session, activeTicketOption)
}

// setActiveTicket records ts as the session's active ticket.
func setActiveTicket(session string, ts *TaskState) error {
	if err := setSessionOption(session, activeTicketOption, ts.Ticket); err != nil {
		return err
	}
	return setSessionOption(session, activeDescriptionOption, ts.Description)
}

// clearActiveTicket removes the active-ticket record, for when every window
// in the session has been stopped and nothing is running anymore.
func clearActiveTicket(session string) error {
	if err := unsetSessionOption(session, activeTicketOption); err != nil {
		return err
	}
	return unsetSessionOption(session, activeDescriptionOption)
}
