# parser

独立的 Go 文本解析基础库，提供不可变原文、物理行索引和带解析标记的游标。
适合按依赖顺序多轮扫描配置或命令输出。语法识别和业务对象由调用方实现。

模块路径：`github.com/gatework/parser`。要求 Go 1.26 或更新版本，仅依赖标准库。

## 安装

```sh
go get github.com/gatework/parser@v0.2.1
```

公开模块，无第三方依赖。版本变更见 [CHANGELOG.md](CHANGELOG.md)，发布步骤见
[发布流程](docs/releasing.md)。

## 使用

```go
content, err := parser.New("rule allow\nobject host\nunknown option\n", parser.Limits{})
if err != nil {
    return err
}

cursor := content.Cursor()
for _, prefix := range []string{"object ", "rule "} {
    cursor.Rewind()
    for line, ok := cursor.NextUnparsed(); ok; line, ok = cursor.NextUnparsed() {
        if !strings.HasPrefix(line.Text, prefix) {
            continue // 保持未解析，留给后续扫描。
        }
        // 在这里解析并保存业务结果；成功后再标记。
        if err := cursor.SetParsed(line.Number, true); err != nil {
            return err
        }
    }
}
fmt.Printf("remaining: %q\n", cursor.UnparsedText())
// remaining: "unknown option\n"
```

示例所需导入为 `fmt`、`strings` 和 `github.com/gatework/parser`。
完整可执行示例见 [example_test.go](example_test.go)。

## 原文与行

`New(text string, limits Limits)` 和 `Read(r io.Reader, limits Limits)` 返回
`(*Content, error)`。构造失败返回 `nil`，避免使用部分读取或超限的输入。

| 接口 | 契约 |
|---|---|
| `Content.Text()` | 完整原始字节 |
| `Content.Len()` | 物理行数 |
| `Content.Line(number)` | 按从 1 开始的行号查询，越界返回零值和 `false` |
| `Content.Span(first, last)` | 一基闭区间内所有行的原始字节，含原换行符；空、反向或越界区间返回 `""` 和 `false` |
| `Content.LineAt(offset)` | 按零基字节偏移查找所在行，使用二分搜索；EOF 或越界返回零值和 `false` |
| `Content.All()` | 返回 `iter.Seq[Line]`，按原文顺序逐行遍历 |
| `Content.Lines()` | 按原文顺序返回新的 `[]Line` |
| `Content.Digest()` | 原始字节的 SHA-256，返回 `[32]byte` |
| `Content.Cursor()` | 新建独立游标，位置为 0，所有行均未解析 |

每个 `Line` 包含 `Number`、`StartByte`、`EndByte`、`Raw` 和 `Text`。
字节区间为 `[StartByte, EndByte)`；`Raw` 包含原来的换行符，`Text` 只去掉换行符。
LF、CRLF 和 CR 都是行结束符，允许混用。缩进、尾部空格、BOM、NUL 和非 UTF-8
字节原样保留；字符集解码和输入业务校验由调用方负责。

空输入为零行；`"a\n"` 为一行；`"a\n\n"` 为两行，第二行为空。
拼接所有行的 `Raw` 总能恢复原文。摘要包含原始换行符，不随解析标记变化。
`LineAt` 使用字节偏移；多字节字符内部的每个偏移均属于同一行，换行符属于它结束的行。
`len(content.Text())` 是 EOF，不属于任何行。
`Span(first, last)` 等于这些行 `Raw` 的拼接，与原文共享存储，适合整块保留 banner、
证书等多行结构。

`Content` 实现 `fmt.Formatter` 和 `slog.LogValuer`：任何 `fmt` 动词（包括 `%v`、
`%+v`、嵌入结构体后打印）和 `slog` 的 Text/JSON 输出都只给出行数、字节数和摘要，
不输出原文，避免把设备口令、密钥写进日志。需要原始字节时显式调用 `Text()`。

`Content` 的零值表示空文本。其内容构造后不可变，可以并发读取。
返回的行和值切片可以由调用方修改，不会改变原文或内部索引。
`Line.Raw` 和 `Line.Text` 共享原文存储；若长期保留一个小字段而希望释放整份配置，
使用 `strings.Clone` 复制该字段。

只需遍历时可使用 `for line := range content.All()`，避免分配完整的 `[]Line`。
同一个迭代器可以重复遍历，也可以通过 `break` 提前结束，不影响任何游标状态。
`Lines()` 仍适合需要持有行切片的调用方。

## 游标与解析标记

| 接口 | 契约 |
|---|---|
| `Next()` | 读取下一物理行，包括已解析行和空行 |
| `Peek()` | 预览下一物理行，不移动位置 |
| `NextUnparsed()` | 向后寻找下一未解析行，包括空行 |
| `Position()` | 下一物理行的零基索引；EOF 时等于行数 |
| `Seek(position)` | 移到 `[0, Len]` 内的零基位置，与 `Position()` 同一值域；越界返回 `ErrLineRange` 且不移动 |
| `Content()` | 游标读取的 `Content`；零值游标返回 nil |
| `Prev()` | 回退一个物理行；已在开头时返回 `false` |
| `Rewind()` | 位置回到 0，保留标记 |
| `Checkpoint()` / `Restore(checkpoint)` | 保存、恢复同一游标的位置，保留标记 |
| `SetParsed(line, parsed)` | 设置一基行号的标记；重复设置相同值无副作用 |
| `SetParsedRange(first, last, parsed)` | 设置一基闭区间内所有行的标记；先校验整个范围，失败时不改变任何状态 |
| `IsParsed(line)` | 查询一基行号的标记 |
| `UnparsedCount()` | 未解析行数，常数时间；为 0 表示全部解析完毕 |
| `Unparsed()` | 返回 `iter.Seq[Line]`，遍历所有当前未解析行，不依赖或改变游标位置 |
| `UnparsedLines()` | 返回所有未解析行的新切片，保留原始行号和顺序 |
| `UnparsedText()` | 按原顺序拼接所有未解析行的 `Raw` |

三种读取操作都返回 `(Line, bool)`；EOF 返回零值和 `false`，不会把空行当成 EOF。
读取、回退和检查点恢复只影响位置。成功解析后显式调用 `SetParsed(number, true)`；
暂缓处理时继续读取即可。
先在整份原文上搜索时，用 `LineAt(offset)` 得到行，再 `Seek(line.Number - 1)`，
下一次 `Next()` 即返回该行。使用 `SetParsed(number, false)` 撤销标记后，需要回到对应
位置或 `Rewind()` 才会重新读到已经越过的行。

检查点绑定创建它的游标，可重复使用。零值检查点和其他游标的检查点均不可恢复。
检查点不保存标记，也不回滚上层业务对象；多行解析可先验证整个块，再调用
`SetParsedRange`。空区间、反向区间或越界区间均返回 `ErrLineRange`。
空白行是否跳过、注释是否算已处理，由调用方决定。

`Unparsed()` 在访问每一行时检查当前标记。循环中修改尚未访问行的标记，会影响本轮
后续结果；已经访问过的行只会在下次遍历时重新检查。迭代器自身不改动标记或位置。
需要固定的一组未解析行时，使用 `UnparsedLines()` 获取快照。

每个游标独立拥有位置和标记，新游标的所有行都处于未解析状态。
`Cursor` 的零值是空游标；游标开始使用后不能按值复制，也不能由多个 goroutine
同时操作。并发解析应共享 `Content` 并分别创建游标。

## 输入限制与错误

`Limits{}` 默认允许 16 MiB 原文和 200,000 行。`MaxBytes`、`MaxLines` 各自为 0 时
使用默认值，正数覆盖默认值，负数无效。空白行计入行数。单行没有独立的 64 KiB
上限，受整个输入的字节限制约束。

`Read` 最多读取 `MaxBytes + 1` 个字节，额外一个字节用于检测超限；它不关闭 Reader。
Reader 返回的数据会复制到模块拥有的文本中。分行索引和摘要在读取期间逐块生成；
一旦观察到第 `MaxLines + 1` 行的起始字节就拒绝输入，不再读取下一块，也不必等待 EOF。
CRLF 跨越读取边界仍算一个行结束符。

只有直接返回的 `io.EOF` 会结束成功读取；包装或组合的 EOF 以及其他
读取错误保留原始错误链并返回 `nil` Content。连续 100 次无进展读取返回
`io.ErrNoProgress`。

可通过 `errors.Is` 判断：

- `ErrInvalidLimits`：负数限制。
- `ErrLimit`：原文字节数或行数超限。
- `ErrInvalidReader`：nil Reader 或违反 `io.Reader` 契约的返回长度。
- `ErrLineRange`：标记操作使用了越界行号或区间，或 `Seek` 使用了越界位置。
- `ErrCheckpoint`：零值或其他游标的检查点。
- Reader 的原始错误，例如 `io.ErrUnexpectedEOF`。

超限还会返回 `*LimitError`，包含 `Resource`（`ResourceBytes` 或 `ResourceLines`）、`Limit`
和 `Observed`。提前停止时，`Observed` 可能只是完整输入大小或行数的下界。
通过 `errors.AsType[*parser.LimitError](err)` 获取详情，原有的
`errors.Is(err, parser.ErrLimit)` 判断仍然有效。一次读取中已经观察到的超限、
读取错误和取消会保留在同一错误链中，不依赖错误消息的文本做判断。

库自身不输出日志，也不把配置原文加入错误消息。Reader 返回的错误会原样保留原因链，
其消息内容由 Reader 决定。

## 取消与超时

`NewWithContext(ctx, text, limits)` 和 `ReadWithContext(ctx, r, limits)` 提供协作式取消。
`ctx` 必须非 nil；已有 `New`、`Read` 使用 `context.Background()`，调用方式保持不变。
上下文只属于本次构造，不会存入 `Content` 或 `Cursor`。

构造会在入口、处理块之间和成功返回前检查取消；读取还会在每次 `Read` 前后检查。
失败时始终返回 nil Content，可以通过 `errors.Is` 检查 `context.Canceled` 或
`context.DeadlineExceeded`。已取消时立即结束，不为收集尚未发现的错误而继续扫描。

上下文不能中断一个已经阻塞的 `io.Reader.Read`。调用方应提供支持取消或 deadline
的 Reader；本库不通过后台 goroutine 读取，也不会为取消而关闭借用的 Reader。

## Perl Content 功能映射

参考实现是 `Firewall::Config::Content::{Role,Static}`。本库保留其读取、标记、
回退和多轮扫描能力，接口按 Go 的所有权模型组织。

| Perl 功能 | Go 用法 |
|---|---|
| `config` / `confContent` | `Content.Lines()` / `Content.Text()`，保留原始换行 |
| `cursor` / `nextLine` / `prevLine` | `Position()` / `Next()` / `Prev()` |
| `goToHead` | `Rewind()` |
| `nextUnParsedLine` | `NextUnparsed()`，解析成功后调用 `SetParsed` |
| `backtrack` | `Prev()`；需要撤销标记时另调 `SetParsed(number, false)` |
| `ignore` | 保持未解析并继续扫描；后续 `Rewind()` 可再次读取 |
| `lineParsedFlags` / 标记查询与设置 | `IsParsed` / `SetParsed` |
| `getUnParsedLines` | `UnparsedText()` 或 `UnparsedLines()` |
| `confSign` | 原始字节的 `Digest()`，采用 SHA-256 |
| `fwId` / `fwName` / `fwType` / `timestamp` | 调用方自己的业务元数据 |

Perl 的读取即标记、隐式空行消费、UTF-8 解码和 MD5 签名格式不作为兼容接口。
本模块没有厂商解析器、业务结果事务或 Firemon 依赖，也不会修改其他模块。

## 验证

```sh
make check
make fuzz
```

`check` 运行外部包行为测试、可执行示例、race detector 和 `go vet`。
`fuzz` 分别对随机分块读取、字节分行与游标操作运行 10 秒 fuzz。测试使用合成文本，
无设备或数据库依赖。性能工作负载、原始结果与比较方法见
[性能记录](docs/performance.md)。
