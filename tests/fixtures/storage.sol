// SPDX-License-Identifier: GPL-3.0

pragma solidity >=0.8.2 <0.9.0;

contract Storage {
    error MyCustomError(uint value, string message);
    event Calculated(address indexed caller, int indexed numA, int indexed numB, int sum);
    uint256 number;

    constructor() payable {
        number = 1337;
    }

    function store(uint256 num) public {
        number = num;
    }

    function retrieve() public view returns (uint256){
        return number;
    }

    function sum(int A, int B) public returns (int) {
        int s = A+B;
        emit Calculated(msg.sender, A, B, s);
        return s;
    }

    function assertError() public pure {
        require(false, "Assert Error Message");
    }

    function customError() public pure {
       revert MyCustomError(5, "Value is too low");
    }
}