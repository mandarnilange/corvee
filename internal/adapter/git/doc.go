// Package git wraps the git CLI via os/exec for sync, merge, add,
// commit, mv, and pull --rebase. The package shells out rather than
// linking a git library so behavior tracks the user's local git
// installation.
package git
