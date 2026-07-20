import 'dart:convert';
void main() {
  final res = jsonDecode('{"a": "b"}');
  print('is Map: ${res is Map}');
  print('is Map<String, dynamic>: ${res is Map<String, dynamic>}');
}
