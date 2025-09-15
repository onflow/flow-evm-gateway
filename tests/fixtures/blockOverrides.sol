// SPDX-License-Identifier: GPL-3.0

pragma solidity >=0.8.2 <0.9.0;

contract BlockOverrides {

    function test() public view {
        // Add some hard-coded if conditions, to simulate overriding
        // certain block header fields. The condition body is a simple
        // way to check that the block header fields were indeed
        // overrided, resulting in higher gas usage.
        if (block.number == 9090) {
            sumValues();
        }

        if (block.timestamp == 1733145057) {
            sumValues();
        }

        if (block.prevrandao == 0x7914bb5b13bac6f621bc37bbf6e406fbf4472aaaaf17ec2f309a92aca4e27fc0) {
            sumValues();
        }

        if (block.coinbase == 0x658Bdf435d810C91414eC09147DAA6DB62406379) {
            sumValues();
        }
    }

    function sumValues() public pure {
        uint sum = 0;
        for (uint i = 0; i < 1000; i++) {
            sum += i;
        }
    }

}
