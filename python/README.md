# agentiik-brick

The Python helper for the Agentiik brick contract, the same surface as the Go and TypeScript ones, name for name. Nothing here is required to write a brick; see the repository's [README](../README.md) and <https://agentiik.github.io/docs#brick>.

```python
import agentiik_brick as brick

try:
    items = brick.items("in")
except ValueError as e:
    brick.refuse(str(e))
for item in items:
    item["data"]["seen"] = True
brick.emit("out", items)
```
