// SPDX-License-Identifier: MIT

// Package e2e runs the built profinetctl binary as a user or script would.
// The tests carry the e2e build tag; run them with make e2e. The cases that
// interrupt a command listen on the Ethernet interface that
// PROFINETCTL_E2E_INTERFACE names, which the binary must be able to open (CI:
// a veth pair, and make e2e E2E_SETCAP=1); without it they are skipped.
package e2e
