import base64
import unittest

from server import parse_inputs


class InputTests(unittest.TestCase):
    def parse(self, value):
        return parse_inputs({"model": "fixture", "input": value}, "fixture", 3)

    def test_mixed_inputs_and_legacy_text(self):
        image = base64.b64encode(b"image bytes").decode()
        self.assertEqual(self.parse([{"text": "query"}, {"image": "data:image/png;base64," + image}]),
                         [("text", "query"), ("image", b"image bytes")])
        self.assertEqual(self.parse("query"), [("text", "query")])

    def test_invalid_image_and_inputs(self):
        for value in [[], [""], [{"image": "http://localhost/private"}],
                      [{"image": "data:image/png;base64,!!!!"}],
                      [{"image": "data:image/png;base64,"}], [{"text": "x", "image": "x"}],
                      [{"audio": "x"}], ["x"] * 17]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.parse(value)

    def test_model_dimension_encoding_must_match(self):
        for changes in [{"model": "other"}, {"dimensions": 4}, {"encoding_format": "base64"}]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                parse_inputs({"model": "fixture", "input": ["query"], **changes}, "fixture", 3)


if __name__ == "__main__":
    unittest.main()
