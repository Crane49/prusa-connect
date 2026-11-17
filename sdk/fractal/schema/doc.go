// Package schema loads the definition of a Fractal interface from a JSON file
// and validates messages against it. Load and Builder.Build return a Registry
// that is never written again, so it is safe for concurrent reads.
//
// One file describes one interface and may declare enumerations. An
// enumeration declared in any file can be used in every file.
//
//	{
//	  "interface": "Chamber1",
//	  "exposure": "upward",
//	  "enums": [
//	    { "name": "NozzleType", "values": ["BRASS", "HARDENED", "STAINLESS"] }
//	  ],
//	  "properties": [
//	    { "name": "tempChamber", "type": "float", "access": "ro", "nullable": true },
//	    { "name": "ledIntensity", "type": "int", "access": "rw" }
//	  ],
//	  "methods": [
//	    {
//	      "name": "SetNozzle",
//	      "params": [{ "name": "nozzleType", "type": "NozzleType" }],
//	      "returns": "bool"
//	    }
//	  ]
//	}
//
// "exposure" is "upward", "downward" or "both". "access" is "ro" or "rw". A
// type is "string", "int", "float", "bool" or a declared enumeration.
// "nullable" and "optional" default to false. A method with no "returns"
// returns nothing. An RPC sends parameters in the order the file lists them.
//
// The values each type accepts:
//
//   - "int": a JSON number with no fraction and no exponent that fits in a
//     signed 64-bit integer. 1 is valid, 1.0 and 1e3 are not.
//   - "float": any JSON number that fits in a 64-bit float.
//   - An enumeration: a JSON string that the enumeration lists.
//   - null: only for a nullable property and for an optional parameter.
//   - The result of a method that declares a return type is never null.
package schema
