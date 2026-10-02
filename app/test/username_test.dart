import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/username.dart';

import 'support/contract.dart';

/// contract/usernames.json: the website and the server agree on the same.
void main() {
  final fixture = contract('usernames.json');

  test("the server's pattern", () {
    expect(usernamePattern.pattern, fixture['pattern']);
  });

  test('valid and invalid usernames, as the server judges them', () {
    for (final u in fixture['valid'] as List) {
      expect(validUsername(u as String), isTrue, reason: u);
    }
    for (final u in fixture['invalid'] as List) {
      expect(validUsername(u as String), isFalse, reason: u);
    }
  });

  test('the suggestion for a name', () {
    for (final s in fixture['suggestions'] as List) {
      final m = s as Map;
      expect(suggestedUsername(m['name'] as String), m['username'], reason: m['name'] as String);
    }
  });
}
