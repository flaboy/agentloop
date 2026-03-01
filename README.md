# agentloop

Shared generic agent loop library.

- No project-specific adapters inside this module.
- Business state mapping and tool/policy adapters must stay in each consuming project.
- Middleware hook entry points are provided via `RegisterHook(point, hook)` with `hook(ctx, next)` execution model.
- Loop events are published to an internal event bus accessible via `runner.EventBus()`.
