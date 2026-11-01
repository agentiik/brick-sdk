// Package brick reads and writes the brick contract: the envelope a brick is given on each
// input port, the parameters and the run context, the secrets mounted for it, one envelope per
// output port, the artifacts it attaches, and the exit code that decides its step's verdict.
//
// Nothing here is required. The contract is files and an exit code, specified at
// https://agentiik.github.io/docs#brick, and a brick that reads and writes them itself is as
// much a brick as one built on this package. What this saves is the part that is easy to get
// subtly wrong: the metadata the runner refuses an envelope for, the size rules it refuses one
// for with exit code 121, and an exit code that is the runner's rather than the brick's.
//
//	items, err := brick.Items("in")
//	if err != nil {
//		brick.Refuse("%s", err)
//	}
//	for i := range items {
//		items[i].Data["seen"] = true
//	}
//	if err := brick.Emit("out", items); err != nil {
//		brick.Fail("%s", err)
//	}
//
// The Python and TypeScript helpers in this repository have the same surface, name for name,
// and the scenarios under conformance/ hold all three to the same behaviour.
//
// It depends on the standard library alone, and on nothing of the engine: a brick is not a
// derivative work of the engine, and a helper that linked it would make every brick built on it
// a downstream of the server it runs under.
package brick
