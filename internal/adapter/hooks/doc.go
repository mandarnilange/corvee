// Package hooks implements domain.PluginRegistry and
// domain.HookDispatcher by walking the workspace's plugins/ tree and
// shelling out to discovered binaries via os/exec. Process spawning,
// file stat, and the stdin/stdout JSON protocol live here so usecase
// code stays free of syscalls per the clean-architecture rule (CLAUDE
// §4 and the depguard config).
package hooks
