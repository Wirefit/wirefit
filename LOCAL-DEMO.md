# Running the demos

The end-to-end consumer/provider demos now live in their own repo:
**[wirefit/examples](https://github.com/wirefit/examples)**. They run against an installed
`wirefit` binary, so they no longer need this source tree.

```bash
go install github.com/wirefit/wirefit/cmd/wirefit@latest   # put `wirefit` on PATH
git clone https://github.com/wirefit/examples wirefit-examples
cd wirefit-examples

./run-demo.sh          # merge-gate: 3 consumers; breaking change blocked, override warns,
                       #             unconsumed removal passes
./run-deploy-demo.sh   # deploy-gate: HEAD green but can-i-deploy blocks prod, then unblocks
```

Everything runs locally: the contract store is a throwaway `mktemp` git repo, and the only
network calls are the pinned Jackson jars from Maven Central and `npm install` for the Zod
consumer (both cached after the first run). Prerequisites (`wirefit` on PATH, JDK 17+, Node,
git, curl) are listed in the examples repo README.

What stays in this repo:

- `conformance/run.sh` — cross-language hash-identity kit (Java/TS/Go produce byte-identical IR)
- `examples/maven-service`, `examples/gradle-service` — build-system classpath integration fixtures (exercised by CI)

For the full presenter script (timings, what to say at each beat) see
[docs/DEMO-FLOW.md](docs/DEMO-FLOW.md).
