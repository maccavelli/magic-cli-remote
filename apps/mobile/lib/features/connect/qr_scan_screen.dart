import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../data/protocol/pair_uri.dart';

/// Full-screen camera QR scanner that returns a [PairPayload] via [Navigator.pop].
class QrScanScreen extends StatefulWidget {
  const QrScanScreen({super.key});

  @override
  State<QrScanScreen> createState() => _QrScanScreenState();
}

class _QrScanScreenState extends State<QrScanScreen> {
  final _controller = MobileScannerController(
    detectionSpeed: DetectionSpeed.normal,
    facing: CameraFacing.back,
    formats: const [BarcodeFormat.qrCode],
  );
  bool _handled = false;
  String? _error;
  bool _permissionDenied = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _onDetect(BarcodeCapture capture) {
    if (_handled || !mounted) return;
    for (final b in capture.barcodes) {
      final raw = b.rawValue;
      if (raw == null || raw.isEmpty) continue;
      final payload = PairPayload.tryParse(raw);
      if (payload == null) {
        setState(
          () => _error = 'Not an mcremote pair QR. Got: ${_short(raw)}',
        );
        continue;
      }
      _handled = true;
      Navigator.of(context).pop(payload);
      return;
    }
  }

  String _short(String s) {
    final t = s.replaceAll('\n', ' ').trim();
    if (t.length <= 48) return t;
    return '${t.substring(0, 45)}…';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Scan pair QR'),
        actions: [
          IconButton(
            tooltip: 'Toggle torch',
            onPressed: () => _controller.toggleTorch(),
            icon: const Icon(Icons.flash_on),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Enter code'),
          ),
        ],
      ),
      body: Stack(
        fit: StackFit.expand,
        children: [
          if (_permissionDenied)
            Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.no_photography_outlined, size: 48),
                    const SizedBox(height: 12),
                    const Text(
                      'Camera permission denied.\nUse Paste or Enter code on the connect screen.',
                      textAlign: TextAlign.center,
                    ),
                    const SizedBox(height: 16),
                    FilledButton(
                      onPressed: () => Navigator.of(context).pop(),
                      child: const Text('Go back'),
                    ),
                  ],
                ),
              ),
            )
          else
            MobileScanner(
              controller: _controller,
              onDetect: _onDetect,
              errorBuilder: (context, error) {
                WidgetsBinding.instance.addPostFrameCallback((_) {
                  if (mounted && !_permissionDenied) {
                    setState(() => _permissionDenied = true);
                  }
                });
                return const SizedBox.shrink();
              },
            ),
          if (!_permissionDenied)
            IgnorePointer(
              child: Center(
                child: Container(
                  width: 240,
                  height: 240,
                  decoration: BoxDecoration(
                    border: Border.all(
                      color: Colors.white70,
                      width: 2,
                    ),
                    borderRadius: BorderRadius.circular(16),
                  ),
                ),
              ),
            ),
          Align(
            alignment: Alignment.bottomCenter,
            child: Container(
              width: double.infinity,
              color: Colors.black54,
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 28),
              child: Text(
                _error ??
                    'Point at the QR from `mcremote pair code` on the host.',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white, fontSize: 14),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
