# projectsetup v0.15.0

This release gives every generated container an init process, so processes orphaned by `dworm exec` sessions are reaped instead of piling up as zombies.

## Changed

- The `app` service in `compose.yaml` sets `init: true`. Its command is still `sleep infinity`, which never reaps children; with `init: true`, Docker's init (`docker-init`) runs as PID 1 and reaps processes reparented to it, such as headless browsers, Git, test runners, or SSH `ProxyCommand` helpers whose `dworm exec` session ended first.
- `check` warns when the `app` service lacks `init: true` or sets it to false. It is a warning, not an error, so `compose.yaml` files from earlier releases still pass.

## Existing projects

Regenerate each project and recreate its container. `dworm down` followed by `dworm up -d` only restarts the existing container without the new setting; `dworm rebuild` recreates it and keeps Compose volumes such as database data:

```bash
projectsetup upgrade
dworm rebuild
```

`rebuild` starts the container. Run `dworm down` afterward if it was stopped before.

If the build fails with `additional privileges requested: pass "--allow=fs.read=..."` for the Dev Container CLI's temporary `Dockerfile-with-features`, your Compose/Bake version requires a filesystem entitlement for that file. Disable the check for this one invocation:

```bash
BUILDX_BAKE_ENTITLEMENTS_FS=0 dworm rebuild
```

To confirm, `docker inspect --format '{{.HostConfig.Init}}' <container>` prints `true`, and PID 1 in the container is `docker-init`.
