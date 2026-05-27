# RunAll Cascade Stop Candidate (Domain Note)

**Bounded Context:** RunAll 服务编排

## Value Objects

- `CascadeStopCandidateStatus` — 下游服务是否应出现在 cascade stop 计划中（stopped/skipped/pending 排除；failed 强制纳入；其余沿用 blocking 语义）

## Domain Services

- `ServiceCascadeOrchestrationService.filterStoppable` — 消费 `isCascadeStopCandidateStatus`
- `ServiceStopPolicyService.isCascadeStopCandidateStatus` — 规则定义

## 与已有模型关系

- `CascadeStepActorResolver` — mixed-ownership 委托（orthogonal）
- `ManagedService.CanStop` — 单点 stop 仍允许 failed 服务被停
