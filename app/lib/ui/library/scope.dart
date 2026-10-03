import 'package:flutter/widgets.dart';

import '../../app.dart';
import '../../data/library.dart';

/// The library the screens below look at: the signed-in phone's, or the folder a PIN shows
/// (screen 46). Thumbnails, the viewer and the player fetch through it, with its key.
class LibraryScope extends InheritedWidget {
  const LibraryScope({super.key, required this.library, required super.child});
  final LibraryRepository library;

  /// The library in scope; without one, the signed-in phone's.
  static LibraryRepository read(BuildContext context) =>
      context.getInheritedWidgetOfExactType<LibraryScope>()?.library ?? Services.read(context).library;

  @override
  bool updateShouldNotify(LibraryScope old) => library != old.library;
}
