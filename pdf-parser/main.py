"""Thothai PDF parser — stateless text extraction microservice.

POST /parse  (multipart: file=<pdf>)  ->  {"text": "...", "method": "pdfplumber"|"openrouter"}

Primary path: pdfplumber (free, ~95% of digital CVs). Fallback: a vision-capable
model via OpenRouter for scanned/image-heavy PDFs where pdfplumber yields too
little text. OpenRouter lets us swap the fallback model without code changes.

No DB, no Redis, no auth, no queue. Loopback-only; called by thothai-worker.
"""

import base64
import io
import os

import pdfplumber
from flask import Flask, jsonify, request

app = Flask(__name__)

# Bounds — backstops against PDF bombs and runaway cost.
MAX_PAGES = int(os.getenv("PDF_MAX_PAGES", "30"))
MAX_TEXT_CHARS = int(os.getenv("PDF_MAX_TEXT_CHARS", "200000"))
# Below this many extracted chars we treat pdfplumber as having failed and fall
# back to the vision model.
MIN_TEXT_THRESHOLD = int(os.getenv("PDF_MIN_TEXT_THRESHOLD", "100"))

# OpenRouter fallback. Model carries the provider prefix (e.g. "openai/...").
OPENROUTER_BASE_URL = os.getenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
FALLBACK_MODEL = os.getenv("PDF_FALLBACK_MODEL", "openai/gpt-4o-mini")
# PDF engine OpenRouter uses to feed the file to the model:
#   "native"      — the vision-capable model reads the file itself (needed for
#                   scanned PDFs; this is the OCR-equivalent path).
#   "mistral-ocr" — dedicated paid OCR.
#   "pdf-text"    — free text-layer extraction; useless here because pdfplumber
#                   already tried exactly that and came up short.
PDF_ENGINE = os.getenv("PDF_OPENROUTER_ENGINE", "native")


@app.get("/health")
def health():
    return jsonify(status="ok", service="pdf-parser")


@app.post("/parse")
def parse():
    f = request.files.get("file")
    if f is None:
        return jsonify(error="missing 'file'"), 400

    data = f.read()
    if not data:
        return jsonify(error="empty file"), 400
    if not data.startswith(b"%PDF-"):
        return jsonify(error="not a PDF"), 400

    text = _extract_pdfplumber(data)
    if text is not None and len(text.strip()) >= MIN_TEXT_THRESHOLD:
        return jsonify(text=text[:MAX_TEXT_CHARS], method="pdfplumber")

    fallback = _extract_fallback(data)
    if fallback is None:
        # pdfplumber gave little/nothing and no fallback available — return what we have.
        return jsonify(text=(text or "")[:MAX_TEXT_CHARS], method="pdfplumber")
    return jsonify(text=fallback[:MAX_TEXT_CHARS], method="openrouter")


def _extract_pdfplumber(data: bytes):
    """Return extracted text, or None if the PDF could not be opened."""
    try:
        with pdfplumber.open(io.BytesIO(data)) as pdf:
            pages = pdf.pages[:MAX_PAGES]
            return "\n".join((p.extract_text() or "") for p in pages)
    except Exception:
        return None


def _extract_fallback(data: bytes):
    """Fallback OCR-style extraction via OpenRouter (Chat Completions + file
    input). Returns None if no API key is configured or the call fails. The
    OpenAI SDK speaks to OpenRouter by pointing base_url at it."""
    api_key = os.getenv("OPENROUTER_API_KEY")
    if not api_key:
        return None
    try:
        from openai import OpenAI

        client = OpenAI(
            base_url=OPENROUTER_BASE_URL,
            api_key=api_key,
            # Optional attribution headers for OpenRouter's dashboard/rankings.
            default_headers={
                "HTTP-Referer": os.getenv("OPENROUTER_REFERER", "https://thothai.app"),
                "X-Title": os.getenv("OPENROUTER_TITLE", "Thothai PDF Parser"),
            },
        )
        b64 = base64.b64encode(data).decode("ascii")
        resp = client.chat.completions.create(
            model=FALLBACK_MODEL,
            messages=[
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "text",
                            "text": "Extract all readable text from this document verbatim. "
                            "Return only the text, no commentary.",
                        },
                        {
                            "type": "file",
                            "file": {
                                "filename": "document.pdf",
                                "file_data": f"data:application/pdf;base64,{b64}",
                            },
                        },
                    ],
                }
            ],
            # Tell OpenRouter how to feed the PDF to the model.
            extra_body={"plugins": [{"id": "file-parser", "pdf": {"engine": PDF_ENGINE}}]},
        )
        return resp.choices[0].message.content
    except Exception:
        return None


if __name__ == "__main__":
    app.run(host="127.0.0.1", port=5001)
