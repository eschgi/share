/// Usernames as the server takes them, and the one suggested for someone without one, as the
/// website suggests it too (contract/usernames.json).
library;

final usernamePattern = RegExp(r'^[\p{L}\p{N}._-]{3,32}$', unicode: true);

/// Whether the server takes it: 3 to 32 letters, digits, dots, dashes or underscores.
bool validUsername(String username) => usernamePattern.hasMatch(username.trim());

/// The name in small letters, with dots for spaces and only what a username may have; empty
/// if that leaves too little.
String suggestedUsername(String name) {
  String trim(String s) => s.replaceAll(RegExp(r'^[._-]+|[._-]+$'), '');
  final s = trim(name
      .trim()
      .toLowerCase()
      .replaceAll(RegExp(r'\s+', unicode: true), '.')
      .replaceAll(RegExp(r'[^\p{L}\p{N}._-]', unicode: true), '')
      .replaceAll(RegExp(r'\.{2,}'), '.'));
  final cut = trim(String.fromCharCodes(s.runes.take(32)));
  return validUsername(cut) ? cut : '';
}
