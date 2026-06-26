#!/usr/bin/env bash
# Mock pi binary for testing.
# - Reads JSONL commands from stdin
# - Emits JSONL events to stdout
# - Maintains a simple state machine
# - Handles "command: response" protocol

# Send agent_start
echo '{"type":"agent_start","sessionID":"mock-123","cwd":"/tmp","model":"mock-model"}'

# Read commands
while IFS= read -r line; do
  # Extract command type using grep/sed (avoid jq dep)
  if echo "$line" | grep -q '"type":"prompt"'; then
    echo '{"type":"message_start","messageId":"m1"}'
    echo '{"type":"message_update","messageId":"m1","content":"hello"}'
    echo '{"type":"message_end","messageId":"m1"}'
    echo '{"type":"turn_end","turnId":"t1"}'
  elif echo "$line" | grep -q '"type":"get_state"'; then
    echo '{"type":"response","commandId":"x","success":true,"data":{"state":"idle"}}'
  fi
done
