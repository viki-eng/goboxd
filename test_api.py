import requests
import json
import sys
from datetime import datetime

# Configuration
BASE_URL = "http://localhost:8080"
OUTPUT_FILE = "api-test-results.txt"

def write_header(f):
    f.write("=" * 80 + "\n")
    f.write(f"goboxd API Test Results – {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n")
    f.write("=" * 80 + "\n\n")

def write_section(f, title):
    f.write("\n" + "-" * 60 + "\n")
    f.write(f"  {title}\n")
    f.write("-" * 60 + "\n")

def log_full_test(f, method, url, test_name, body=None, response=None, error=None):
    """Write full test details to file."""
    f.write(f"\n[TEST] {test_name}\n")
    f.write(f"Method: {method}\n")
    f.write(f"URL: {url}\n")

    if body is not None:
        f.write(f"Request Body:\n{json.dumps(body, indent=2)}\n")

    if error:
        f.write(f"Error: {error}\n")
        f.write(f"Status: FAIL\n")
    else:
        f.write(f"Status Code: {response.status_code}\n")
        f.write(f"Response Headers:\n{json.dumps(dict(response.headers), indent=2)}\n")
        try:
            resp_json = response.json()
            f.write(f"Response Body (JSON):\n{json.dumps(resp_json, indent=2)}\n")
        except:
            f.write(f"Response Body (text):\n{response.text}\n")

        # Determine pass/fail based on expected status (we'll store that separately)
    f.write("\n")

def test_endpoint(f, method, url, expected_status, test_name, body=None):
    """Test a single endpoint and log full details."""
    try:
        if method == "GET":
            resp = requests.get(url, timeout=10)
        elif method == "POST":
            resp = requests.post(url, json=body, timeout=10)
        else:
            raise ValueError("Unsupported method")

        # Log full details
        log_full_test(f, method, url, test_name, body, resp, error=None)

        # Determine pass/fail
        if resp.status_code == expected_status:
            status = "PASS"
        else:
            status = "FAIL"
        f.write(f"Expected status: {expected_status}, Got: {resp.status_code} – {status}\n")
        return resp
    except Exception as e:
        log_full_test(f, method, url, test_name, body, error=str(e))
        f.write(f"Expected status: {expected_status}, Got: Exception – FAIL\n")
        return None

def main():
    # Clear output file and write header
    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        write_header(f)

    print("\n=== Starting detailed API tests ===\n")
    print(f"Results will be saved to {OUTPUT_FILE}\n")

    with open(OUTPUT_FILE, "a", encoding="utf-8") as f:
        # 1. /healthz
        write_section(f, "GET /healthz")
        test_endpoint(f, "GET", f"{BASE_URL}/healthz", 200, "GET /healthz")

        # 2. /readyz
        write_section(f, "GET /readyz")
        test_endpoint(f, "GET", f"{BASE_URL}/readyz", 200, "GET /readyz")

        # 3. /info
        write_section(f, "GET /info")
        test_endpoint(f, "GET", f"{BASE_URL}/info", 200, "GET /info")

        # 4. POST /run - Python success
        write_section(f, "POST /run (Python success)")
        py_body = {
            "language": "py3",
            "source": "print('Hello, World!')",
            "tests": [{"stdin": "", "expected_stdout": "Hello, World!"}]
        }
        resp = test_endpoint(f, "POST", f"{BASE_URL}/run", 200, "POST /run (Python success)", py_body)
        if resp and resp.status_code == 200:
            data = resp.json()
            if data.get("status") == "accepted":
                f.write("Status verification: 'accepted' – PASS\n")
            else:
                f.write(f"Status verification: '{data.get('status')}' – FAIL\n")

        # 5. POST /run - C++ success
        write_section(f, "POST /run (C++ success)")
        cpp_body = {
            "language": "cpp",
            "source": '#include <iostream>\nint main() {\n    std::cout << "Hello, World!" << std::endl;\n    return 0;\n}',
            "tests": [{"stdin": "", "expected_stdout": "Hello, World!"}]
        }
        resp = test_endpoint(f, "POST", f"{BASE_URL}/run", 200, "POST /run (C++ success)", cpp_body)
        if resp and resp.status_code == 200:
            data = resp.json()
            if data.get("status") == "accepted":
                f.write("Status verification: 'accepted' – PASS\n")
            else:
                f.write(f"Status verification: '{data.get('status')}' – FAIL\n")

        # 6. POST /run - invalid language (expect 400)
        write_section(f, "POST /run (invalid language – expect 400)")
        invalid_body = {
            "language": "nonexistent",
            "source": "print('hello')",
            "tests": [{"stdin": "", "expected_stdout": "hello"}]
        }
        test_endpoint(f, "POST", f"{BASE_URL}/run", 400, "POST /run (invalid language – expect 400)", invalid_body)

        # 7. POST /run - missing source (expect 400)
        write_section(f, "POST /run (missing source – expect 400)")
        missing_source_body = {
            "language": "py3",
            "tests": [{"stdin": "", "expected_stdout": "hello"}]
        }
        test_endpoint(f, "POST", f"{BASE_URL}/run", 400, "POST /run (missing source – expect 400)", missing_source_body)

        # 8. POST /run - oversize source (expect 400)
        write_section(f, "POST /run (oversize source – expect 400)")
        oversize_body = {
            "language": "py3",
            "source": "x" * 262145,
            "tests": [{"stdin": "", "expected_stdout": "hello"}]
        }
        test_endpoint(f, "POST", f"{BASE_URL}/run", 400, "POST /run (oversize source – expect 400)", oversize_body)

        # 9. POST /run - too many tests (expect 400)
        write_section(f, "POST /run (too many tests – expect 400)")
        many_tests_body = {
            "language": "py3",
            "source": "print('hello')",
            "tests": []
        }
        for _ in range(51):
            many_tests_body["tests"].append({"stdin": "", "expected_stdout": "hello"})
        test_endpoint(f, "POST", f"{BASE_URL}/run", 400, "POST /run (too many tests – expect 400)", many_tests_body)

        # 10. POST /run - disallowed flag (expect 400)
        write_section(f, "POST /run (disallowed flag – expect 400)")
        disallowed_flag_body = {
            "language": "cpp",
            "source": "int main() { return 0; }",
            "build": {"flags": ["-fplugin=evil.so"]},
            "tests": [{"stdin": "", "expected_stdout": ""}]
        }
        test_endpoint(f, "POST", f"{BASE_URL}/run", 400, "POST /run (disallowed flag – expect 400)", disallowed_flag_body)

        f.write("\n" + "=" * 80 + "\n")
        f.write("=== Tests complete ===\n")
        f.write("=" * 80 + "\n")

    print(f"\n✅ Detailed results saved to {OUTPUT_FILE}")
    print("Open it with any text editor to see full request/response details.")

if __name__ == "__main__":
    main()