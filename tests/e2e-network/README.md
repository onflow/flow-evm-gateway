# E2E Network test

The E2E network tests are meant to test a deployment on live networks.

### Running tests

1. To run the tests you need to have `node` installed.
2. Run `npm install` to install all needed packages
3. Run the tests `RPC_HOST={host} USER_PRIVATE_KEY={key} npm test`

The `RPC_HOST` should be one of the supported networks: `local`, `testnet`.

The `USER_PRIVATE_KEY` should be a valid EVM private key to an account that is funded with Flow. 
The easiest way to achieve that is to generate a random EVM key and address and 
fund it using Flow faucet: https://faucet.flow.com/fund-account

You can generate random address and private key by running: `GENERATE=1 npm test`,
this should output an address and key, and you should fund it using the faucet.

Example output:
```
GENERATE=1 npm test

Private Key: 0x77a4c5692d4053edb48e6229e3547777039f389cb1532c2c2ef879233a7f5bb0
Address: 0x4F463dB60104266199F077e7B59EdB305E8e23f9
```


