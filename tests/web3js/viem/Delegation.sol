pragma solidity ^0.8.20;
 
contract Delegation {
  event Log(string message);
 
  function initialize() external payable {
    emit Log('Hello, world!');
  }
 
  function ping() external {
    emit Log('Pong!');
  }
}
