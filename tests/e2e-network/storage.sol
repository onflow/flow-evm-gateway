// SPDX-License-Identifier: GPL-3.0

pragma solidity >=0.8.2 <0.9.0;

contract Storage {
    event NewStore(address indexed caller, uint256 indexed value);

    uint256 number;

    constructor() payable {
        number = 1337;
    }

    function store(uint256 num) public {
        number = num;
        emit NewStore(msg.sender, num);
    }

    function retrieve() public view returns (uint256){
        return number;
    }
}