# MiniMesh Failure Test Report

可审计的实测报告由以下命令生成：

```sh
make demo-stage16
```

输出位于 `artifacts/stage16/FAILURE_TEST_REPORT.md`，逐项记录场景、PASS/FAIL 和截断后的
运行证据。覆盖 Backend、Sidecar、Control Plane、etcd、network delay、packet loss、
80% error、slow response，以及 LB/retry/breaker/rate-limit/config recovery/trace/metrics。

本文件定义报告入口，不伪造尚未在当前机器执行的 PASS。提交或发布验收结果时，应附上
当次生成的 artifact、运行环境和容器日志；详细断言见
[Stage-16-故障注入与最终验收.md](Stage-16-故障注入与最终验收.md)。
