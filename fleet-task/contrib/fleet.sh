# fleet-task shell integration.
#
# `fleet-task jump` fzf-selects a worktree and prints its path on stdout
# (and only that). A subprocess can't change its parent shell's working
# directory, so the `cd` has to happen in the calling shell itself -- this
# `fj` function wraps `fleet-task jump` and does the `cd` for you.
#
# Source this file from your ~/.bashrc or ~/.zshrc, e.g.:
#   source /path/to/fleet-task/contrib/fleet.sh
#
# Then just run `fj` in any shell to jump to a task's worktree.

fj() { local d; d=$(fleet-task jump) && cd "$d"; }
