# 架构设计备忘录

executor: 负责实际执行 ping, traceroute 等工作。以 HTTP 接口实现，返回一个 line JSON stream，从 URL query 获取参数。

proxy: 负责服务发现，具体地，proxy 向 hub 宣告 executor 的存在，让 hub 知道都有哪些 executor 存在，以及如何向这些 executor 发起通信。

hub: 负责接收任务并分拆任务给当前能执行工作的 executor（可能有 0 个、一个或多个 executor），等待 proxy 向自己注册（proxy 一般而言是代表 executor 向 hub 注册）。
