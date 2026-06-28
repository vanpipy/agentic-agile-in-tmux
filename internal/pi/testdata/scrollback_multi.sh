#!/usr/bin/env bash
# Mock binary for testing multi-line scrollback capture.
# Writes 10 lines in one printf chunk, then sleeps to keep the PTY
# alive long enough for pane.Stop() (SIGKILL) to clean up.
#
# Content matches the legacy /tmp/multi_pi.sh that this test originally
# referenced via a hardcoded path (violating §5.6 anti-patterns).
# Promoting the script into the repo fixes the clean-CI failure mode.

printf 'Line 1 content\nLine 2 content\nLine 3 content\nLine 4 content\nLine 5 content\nLine 6 content\nLine 7 content\nLine 8 content\nLine 9 content\nLine 10 content\n'
sleep 60
