# 文本配置解析性能记录

2026-11-07，本轮保持现有 Content/Cursor API，增加增量索引、协作式取消、
迭代器、字节位置定位和区间标记。性能结论来自同机、同工具链的基线与最终实现对照。

## 工作负载与测量方法

环境：Apple M5，darwin/arm64，macOS 26.6.2，Go 1.27.1，`GOWORK=off`。
`benchstat` 使用 `golang.org/x/perf@v0.0.0-20261009192801-be2c69fb417e`。
Go 1.26.0 仅做兼容性测试和 vet，不与 Go 1.27.1 混合比较性能。

| 工作负载 | 输入 |
|---|---|
| `small` | 1,024 字节，64 行，每行 `"key=value      \n"` |
| `short_lines` | 3,400,000 字节，200,000 行，每行 `"set option value\n"` |
| `long_line` | 16 MiB 单行，以 CRLF 结束 |
| `sparse` | 仅每第 10 行保持未解析；大输入剩余 20,000 行 |
| `dense` | 仅每第 10 行已解析；大输入剩余 180,000 行 |

基准使用 `b.Loop()`，输入构造、游标创建和初始标记不计入计时。
遍历基准均累计 `len(line.Text)`，避免把不同工作量作比较。
`Read` 使用可重置的 `strings.Reader`；这些数字不包含文件、网络或设备 I/O。

在修改核心实现前保存基线测试二进制，完成修改后保存最终二进制。
每个版本运行 10 次，每次每项至少 100 ms，`GOMAXPROCS=1`。
两个二进制串行交替执行，每轮交换先后顺序；没有同时运行本任务的测试、fuzz 或其他基准。

```sh
GOWORK=off go test -c -o /tmp/parser-bench.ndjIvc/final.test .

# 每个二进制的每次采样参数相同；共采集 10 轮。
/tmp/parser-bench.ndjIvc/final.test \
  -test.run='^$' -test.bench=. -test.benchmem \
  -test.benchtime=100ms -test.count=1 -test.cpu=1

# 在 docs/benchmarks 下汇总已保存的完整样本。
benchstat baseline.txt final.txt
```

基线二进制 SHA-256：
`f081e6e6672d628f86981af5cec83aced4c0f5fd3823b54328da72dc260d1168`。
最终二进制 SHA-256：
`d3df48b4a04a6e8325e2ee5cae8daafafe752f1d2435256d890b1e797c9baf81`。
二进制保存在本次会话的临时目录中；持久记录包括
[基线样本](benchmarks/baseline.txt)、[最终样本](benchmarks/final.txt) 和
[完整 benchstat 对照](benchmarks/comparison.txt)。

## 相同 API 的结果

耗时为 10 个样本的中位数。`B/op` 是每次操作累计分配的堆字节数，不是峰值 RSS，
也不包含基准预先构造的输入。表中内存单位使用二进制单位。

| 操作 | 耗时：基线 → 最终 | B/op：基线 → 最终 | allocs/op：基线 → 最终 |
|---|---|---|---|
| `New / small` | 1.016 → 1.018 µs，差异不显著 | 2.070 → 2.070 KiB | 9 → 9 |
| `New / short_lines` | 2.831 → 2.819 ms，差异不显著 | 11.232 → 8.013 MiB | 33 → 33 |
| `New / long_line` | 12.73 → 12.70 ms，差异不显著 | 16,384.09 → 32.09 KiB | 3 → 3 |
| `Read / short_lines` | 3.328 → 3.328 ms，差异不显著 | 28.20 → 24.95 MiB | 52 → 51 |
| `Read / long_line` | 15.10 → 14.63 ms，差异不显著 | 99.24 → 83.24 MiB | 29 → 28 |
| `UnparsedLines / short_lines / sparse` | 714.9 → 320.9 µs，减少 55.11% | 5.656 → 1.070 MiB | 22 → 1 |
| `UnparsedLines / short_lines / dense` | 11.923 → 3.930 ms，减少 67.03% | 48.812 → 9.617 MiB | 31 → 1 |
| `UnparsedText / short_lines / sparse` | 152.8 → 147.2 µs，减少 3.66% | 1,501.4 → 336.0 KiB | 26 → 1 |
| `UnparsedText / short_lines / dense` | 1.2834 → 0.9838 ms，减少 23.34% | 16.013 → 2.922 MiB | 36 → 1 |

构造和读取的主要收益是减少分配，并在流式读取期间执行行数限制。本次样本未证明
这两类操作的耗时改善。大输入稀疏 `UnparsedText` 的小幅耗时差异不作为主要收益；
其分配量减少约 78%。`Lines` 和 `Cursor.Next` 的实现保持不变，少量计时差异不归因于优化。

## 遍历接口的选择

以下是最终实现中完成相同累计工作量的不同接口。迭代器是新增接口，因此这些是
接口选择的成本对照，并非旧调用方无需修改就能获得的收益。

| 工作负载 | 物化切片 | 直接迭代 | 迭代时 B/op、allocs/op |
|---|---|---|---|
| 全部 200,000 行 | `Lines`: 3.772 ms，10.69 MiB | `All`: 913.0 µs | 0、0 |
| 未解析 20,000 行 | `UnparsedLines`: 320.9 µs，1.070 MiB | `Unparsed`: 118.1 µs | 0、0 |
| 未解析 180,000 行 | `UnparsedLines`: 3.930 ms，9.617 MiB | `Unparsed`: 906.0 µs | 0、0 |

仅遍历时选择 `All` 或 `Unparsed`；需要持有行快照时选择切片接口。
`Unparsed` 在遍历过程中读取当前标记，快照语义仍由 `UnparsedLines` 提供。
零分配结论限于这些直接 range 调用；若调用方保存闭包或收集结果，仍可能分配。

## 成本来源与实现选择

- 原文使用 `string`，行结束位置使用 `[]int`，每个游标持有独立 `[]bool`。
  不引入映射表、位图、对象池或缓存，查询行为和所有权保持明确。
- `New` 使用最多 32 KiB 的临时缓冲区增量计算 SHA-256，消除整份原文的 `[]byte` 复制。
  `Read` 一次读取同时完成索引与摘要，不在 EOF 后调用 `New` 再扫描。
- `Read` 将输入写入自己拥有的 Builder，再用本次新增的字符串视图建索引。
  索引器只保存偏移，避免每个读取块再分配临时字符串。
- 摘要由内部 `contentBuilder` 统一计算并写入固定数组。v0.1.1 起哈希状态经由
  `hash.Hash` 字段调用，会逃逸到堆上，每次构造多一次约 128 B 的分配；
  见下文“v0.1.1 统一导入路径”。没有通过自定义接口扩大对标准库实现的依赖。
- `UnparsedLines` 先统计数量再一次分配；`UnparsedText` 先统计原文字节数再 Grow，
  按索引拼接源区间，不构建不需要的 `Line.Text`。

增量索引与摘要仍需扫描所有接受的字节，行索引也会随行数增长；流式读取不是常量内存。
此库保留完整配置文本，`Read` 的 Builder 扩容仍有可见的累计分配。

## 噪声与验证边界

首次测量最终实现时，未修改的 `Lines` 等控制项也出现数倍变慢，样本波动明显。
该轮计时没有用于上面的结论，原始数据保存在
[初始基线](benchmarks/initial-baseline.txt) 和
[受干扰的最终样本](benchmarks/initial-final-noisy.txt)。随后按预定的 10 轮交替方案，
对两个已冻结的二进制整体重新采样，没有挑选单个最快样本。
交替结果仍有环境波动，完整对照保留了置信区间和显著性判断。
新增接口造成两列测试集合不同，因此不使用总体 geomean 作为性能结论。

最终源码已通过 `make check`（外部包测试、可执行示例、race、vet）、`make fuzz`
（两组各 10 秒）以及 Go 1.26.0 的测试与 vet。回归覆盖跨块 CRLF、提前行数拒绝、
字节探测边界、错误链、多种取消时机、Reader 所有权、字节定位、迭代器复用和区间失败无修改。
fuzz 将游标操作、两字节行号和标记分别生成，并比较随机分块读取与字符串构造的结果。

这是本机合成配置测试，没有设备或网络性能验收。上下文取消仍是协作式的：
已经阻塞的 Reader 需要由调用方提供取消或 deadline 能力。

## v0.1.1 统一导入路径

2026-11-08，`New` 与 `Read` 改为共用内部 `contentBuilder`，统一执行字节与行数限制、
分行、摘要和取消检查；公开 API 与行为不变。环境、工作负载和 10 轮交替方法同上，
仅采集 `BenchmarkNew` 与 `BenchmarkRead`。

基线二进制（v0.1.0）SHA-256：
`96f1b396f5bc62619a8f6bbf5b5bbb34700be215e544e2606245279b3a3a9c96`。
最终二进制 SHA-256：
`6ae0003d89a00666cbd21e7035c55559fb6c07181b33279cb9f0011896b569e4`。
原始数据见 [基线样本](benchmarks/builder-baseline.txt)、
[最终样本](benchmarks/builder-final.txt) 和 [benchstat 对照](benchmarks/builder-comparison.txt)。

| 操作 | 耗时：基线 → 最终 | B/op：基线 → 最终 | allocs/op：基线 → 最终 |
|---|---|---|---|
| `New / small` | 1.307 → 1.315 µs，差异不显著 | 2.070 → 2.195 KiB | 9 → 10 |
| `New / short_lines` | 3.658 → 3.493 ms，差异不显著 | 8.013 → 8.013 MiB | 33 → 34 |
| `New / long_line` | 15.94 → 15.34 ms，差异不显著 | 32.09 → 32.21 KiB | 3 → 4 |
| `Read / small` | 3.111 → 2.924 µs，差异不显著 | 34.07 → 35.20 KiB | 10 → 12 |
| `Read / short_lines` | 4.425 → 4.206 ms，差异不显著 | 24.95 → 24.98 MiB | 51 → 53 |
| `Read / long_line` | 19.75 → 19.06 ms，差异不显著 | 83.24 → 83.27 MiB | 28 → 30 |

耗时均无显著差异。新增分配来自两处：哈希状态逃逸（`New`、`Read` 各一次），
以及 `Read` 为 SHA-256 复制字符串分块所用的缓冲区。该缓冲区按实际分块大小几何增长，
上限 32 KiB，不随原文整体增长。首轮测量中缓冲区固定为 32 KiB，`New / small` 的
B/op 增至 33.3 KiB；该实现未发布，已改为按需分配后重新采样，上表为重新采样结果。
