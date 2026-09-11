# brick-sdk

Thin helpers that read and write the envelope contract, for Go, Python and TypeScript.

Convenience only. The contract is a JSON envelope on standard input and output, files
under `/agk/in` and `/agk/out`, and an exit code: it stands without a library, and a
brick that parses the envelope itself is not a second-class brick. These exist so that
the common case is three lines rather than thirty, and nothing here is ever required to
write a brick.

Nothing is written yet. The contract is specified at <https://agentiik.github.io/docs>,
and the envelope schema comes from [`agentiik/schemas`](https://github.com/agentiik/schemas).

## Licence

Apache-2.0, see [LICENSE](LICENSE). This code ends up compiled into other people's bricks,
which is the whole reason it cannot be copyleft. [LICENSING.md](https://github.com/agentiik/.github/blob/main/LICENSING.md) has the
reasoning.

## Contributing

[CONTRIBUTING.md](https://github.com/agentiik/.github/blob/main/CONTRIBUTING.md), under the Developer Certificate of Origin 1.1.
