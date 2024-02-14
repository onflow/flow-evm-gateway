## Emulator Integration Tests

The emulator integration tests startup all the components needed for running EVM gateway 
and use the emulator and scripts to generate data which is after checked whether it has 
been correctly handled.

### Running Tests
Build and run main.go with selected test:
```
go run ./main.go --test {NAME}
```

// TODO right now the --test flag is not supported and a default test is run.