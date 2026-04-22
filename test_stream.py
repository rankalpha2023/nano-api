import requests
import json
import sys

def test_stream(url="http://localhost:8080/v1/chat/completions"):
    print("Testing streaming request...")
    print(f"URL: {url}")
    print()

    headers = {
        "Content-Type": "application/json"
    }

    data = {
        "model": "high2",
        "messages": [
            {
                "role": "user",
                "content": "Hello, how are you?"
            }
        ],
        "stream": True,
        "max_tokens": 100
    }

    try:
        response = requests.post(url, headers=headers, json=data, stream=True, timeout=60)
        print(f"Status code: {response.status_code}")
        print(f"Response headers: {dict(response.headers)}")
        print()
        print("Response content:")

        for line in response.iter_lines():
            if line:
                line = line.decode('utf-8')
                print(line)
                sys.stdout.flush()

        print()
        print("Stream finished.")

    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test_stream()
