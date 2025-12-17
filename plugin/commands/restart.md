# Restart Claude Mnemonic Worker

Restart the claude-mnemonic worker process. Use this command when experiencing issues with the memory system.

## Instructions

1. Call the restart API endpoint using curl:
   ```bash
   curl -X POST http://127.0.0.1:37777/api/restart
   ```

2. Wait a moment for the worker to restart (typically 1-2 seconds)

3. Verify the worker is running by checking the version:
   ```bash
   curl -s http://127.0.0.1:37777/api/version
   ```

4. Report the result to the user, including the version number from the response.

If the restart fails, suggest the user check `/tmp/claude-mnemonic-worker.log` for errors.
