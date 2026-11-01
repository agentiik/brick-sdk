# brick-sdk

Thin helpers that read and write the envelope contract, for Go, Python and TypeScript.

Convenience only. The contract is a JSON envelope on standard input and output, files
under `/agk/in` and `/agk/out`, and an exit code: it stands without a library, and a
brick that parses the envelope itself is not a second-class brick. These exist so that
the common case is three lines rather than thirty, and nothing here is ever required to
write a brick.

The contract is specified at <https://agentiik.github.io/docs#brick>, and the envelope schema comes from [`agentiik/schemas`](https://github.com/agentiik/schemas).

## Go

```go
import "github.com/agentiik/brick-sdk/brick"

items, err := brick.Items("in")
if err != nil {
	brick.Refuse("%s", err)
}
if err := brick.Emit("out", items); err != nil {
	brick.Fail("%s", err)
}
```

| | |
| --- | --- |
| `Read(port)`, `Items(port)` | The envelope on an input port, from `/agk/in/<port>/envelope.json`, or standard input for `in` when no file is mounted. A port nobody fed is an empty batch. |
| `Emit(port, items)` | Writes `/agk/out/ports/<port>.json` with the metadata the container was given, after spilling every value above `inline_max_bytes` to an artifact and refusing an envelope above `envelope_max_bytes` or `max_items`, so the runner never refuses it with 121. |
| `Attach(port, name, mediaType, content)` | Writes an artifact under `/agk/out/files/` and answers its `files[]` entry. |
| `Params()`, `Param(name, &v)` | `/agk/params.json`, telling an absent parameter from a zero one. |
| `Context()` | `/agk/run.json` and the `AGK_*` variables: the run, the step, the attempt, the shard, the deadline, the declared ports. |
| `Secret(name)` | `/agk/secrets/<name>`, and never the environment. |
| `Fail`, `Retry`, `Refuse`, `Exit` | Leave with 1, 100 or 120. A code outside 1 to 120 becomes 1, since the rest is the runner's or the runtime's. |

Numbers in `data` are decoded as `json.Number`, so an amount or an identifier keeps its digits. The module depends on the standard library alone.

## Python

```python
import agentiik_brick as brick

items = brick.items("in")
brick.emit("out", items)
```

The same functions in Python's spelling: `read`, `items`, `emit`, `attach`, `params`, `param(name, default)`, `context`, `secret`, `fail`, `retry`, `refuse`, `exit`, `exit_code`. Items are plain dictionaries, a refused envelope raises `TooLarge`, and an absent secret is `None`. It supports Python 3.9 and later, and the standard library is all it imports.

## One behaviour in three languages

`conformance/scenarios.json` holds what every helper does: the exit codes, how a spilled value is named, encoded and digested, which envelopes are refused. Each language's tests read it, so a scenario added there is a scenario all three are held to.

## Licence

Apache-2.0, see [LICENSE](LICENSE). This code ends up compiled into other people's bricks,
which is the whole reason it cannot be copyleft. [LICENSING.md](https://github.com/agentiik/.github/blob/main/LICENSING.md) has the
reasoning.

## Contributing

[CONTRIBUTING.md](https://github.com/agentiik/.github/blob/main/CONTRIBUTING.md), under the Developer Certificate of Origin 1.1.
