# Sanitization Notes

This codebase has been sanitized to remove sensitive IP addresses and default security keys before publication.

## Changes Made:

1. **main.go**:
   - Changed default server URL from `http://120.77.94.57:5002/clip` to `http://localhost:5002/clip`
   - Changed default key from `0123456789abcdef` to `your-secret-key-here`
2. **server_design.md**: Changed test server reference from `http://120.77.94.57:5002` to `http://localhost:5002`
3. **Test files** (poll_test.go, client_test.go): Changed test key from `0123456789abcdef` to `test-secret-key`

## Usage:

Users will need to:
1. Set up their own server endpoint
2. Configure a secure shared secret key
3. Run the application with appropriate flags:
   ```
   clipsync -http "http://your-server:port/clip" -key "your-actual-secret-key"
   ```

## Security Note:

The placeholder keys (`your-secret-key-here` and `test-secret-key`) are intentionally obvious placeholders that must be replaced with secure values before deployment.