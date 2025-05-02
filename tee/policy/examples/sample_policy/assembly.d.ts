// Type definitions for AssemblyScript
// This file helps TypeScript linting tools understand AssemblyScript's special types

// Create an AssemblyScript namespace to avoid conflicts with regular TypeScript
declare namespace AS {
  // Basic numeric types
  type i8 = number;
  type i16 = number;
  type i32 = number;
  type i64 = number;
  type u8 = number;
  type u16 = number;
  type u32 = number;
  type u64 = number;
  type f32 = number;
  type f64 = number;
  type bool = boolean;
  type usize = number; // Platform-dependent unsigned size
  
  // Array buffer type
  interface ArrayBuffer {
    readonly byteLength: number;
  }
  
  // Uint8Array with AssemblyScript specifics
  interface Uint8Array {
    dataStart: i32;
    readonly buffer: ArrayBuffer;
    readonly length: number;
  }
}

// Make these type aliases available globally for type checking
declare type i8 = AS.i8;
declare type i16 = AS.i16;
declare type i32 = AS.i32;
declare type i64 = AS.i64;
declare type u8 = AS.u8;
declare type u16 = AS.u16;
declare type u32 = AS.u32;
declare type u64 = AS.u64;
declare type f32 = AS.f32;
declare type f64 = AS.f64;
declare type usize = AS.usize;

// AssemblyScript builtins
declare function store<T>(ptr: i32, value: T): void;
declare function load<T>(ptr: i32): T;
declare function memory(offset: i32, size: i32): ArrayBuffer;
declare function __new(size: i32, id: i32): i32;
declare function idof<T>(): i32;
declare function changetype<T>(value: any): T;

// String utilities
interface StringConstructor {
  UTF8: {
    encode(str: string): ArrayBuffer;
    decode(buf: ArrayBuffer): string;
  }
  fromUTF8(ptr: i32, len: i32): string;
}

// Memory interfaces
interface ArrayBufferConstructor {
  __new(length: i32, id: i32): ArrayBuffer;
}

// Add wrap method to Uint8Array constructor
interface Uint8ArrayConstructor {
  wrap(buffer: ArrayBuffer): Uint8Array;
}

// JSON module stub
declare module "assemblyscript/lib/assembly/json" {
  export class JSON {
    static parse(str: string): any;
    static stringify(obj: any): string;
  }
}

// Memory operations
declare namespace memory {
  function copy(dest: i32, src: i32, n: i32): void;
  function fill(dest: i32, value: u8, n: i32): void;
  function compare(lhs: i32, rhs: i32, n: i32): i32;
  function data(offset?: number): number;
  const buffer: ArrayBuffer;
}
