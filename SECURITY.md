# Security

Do not commit API keys, gateway credentials, database passwords, proxy secrets, or private tokens.

Committed YAML must reference deployment secrets through environment placeholders:

```yaml
apiKey: ${LUBAN_LLM_API_KEY}
app_secret: ${IPS_APP_SECRET}
```

`make check` runs `secrets_test.go`, which rejects literal values in common secret fields under `conf/`.

If a secret was committed, removing it from the latest revision is not enough:

1. Revoke and rotate the secret immediately.
2. Replace the committed value with an environment reference.
3. Coordinate any Git history rewrite with repository owners before force-pushing rewritten branches.
