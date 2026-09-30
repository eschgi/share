import 'dart:convert';
import 'dart:io';

/// A fixture from contract/ at the top of the repository: the same shapes the server's tests
/// check its responses against.
Map<String, dynamic> contract(String name) =>
    jsonDecode(File('../contract/$name').readAsStringSync()) as Map<String, dynamic>;

/// The example response of an endpoint fixture.
Map<String, dynamic> contractResponse(String name) => (contract(name)['response'] as Map).cast<String, dynamic>();
