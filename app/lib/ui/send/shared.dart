import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/platform.dart';

/// Sends the files shared into the app from other apps ("Send with Share") as soon as they are
/// there: in the signed-in app with the phone's key, in PIN sending with the PIN. [onShared]
/// brings the sending into view first. Signed in with a second folder, they wait there for the
/// person to say which folder they go into.
class SharedSender extends StatefulWidget {
  const SharedSender({super.key, required this.auth, this.onShared, required this.child});
  final SendAuth auth;
  final VoidCallback? onShared;
  final Widget child;

  @override
  State<SharedSender> createState() => _SharedSenderState();
}

class _SharedSenderState extends State<SharedSender> {
  late final AppServices _services = Services.read(context);
  bool _sending = false;

  bool get _signedIn => widget.auth == SendAuth.device;

  @override
  void initState() {
    super.initState();
    _services.shared.addListener(_arrived);
    if (_signedIn) _services.folders.addListener(_send); // e.g. the folders known at last
    WidgetsBinding.instance.addPostFrameCallback((_) => _arrived());
  }

  @override
  void dispose() {
    _services.shared.removeListener(_arrived);
    _services.folders.removeListener(_send);
    super.dispose();
  }

  /// Files were shared: the sending comes into view, and they go if they may.
  void _arrived() {
    if (_sending || !mounted || _services.shared.value == 0) return;
    widget.onShared?.call();
    _send();
  }

  Future<void> _send() async {
    if (_sending || !mounted || _services.shared.value == 0) return;
    final folders = _services.folders;
    if (_signedIn && folders.asksForFolder) return; // they wait on the Send tab
    _sending = true;
    try {
      await _services.platform.sendShared(widget.auth, folder: _signedIn ? folders.sendTo!.id : null);
    } on Exception {
      // They stay waiting; the next share or start tries again.
    }
    _services.shared.value = await _services.platform.sharedCount();
    _sending = false;
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
