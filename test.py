import os
import requests
import json

url = "http://127.0.0.1:8080/v1/chat/completions"
model = 'high'

headers = {
    "Content-Type": "application/json"
}

# 构建请求体，指定模型、用户输入和提示词
payload = {
    "model": model,          # 指定要使用的模型
    "messages": [
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "你好，请介绍一下你自己。"}
    ],
    "stream": True,
    "temperature": 0.7,                    # 可选参数，控制随机性
    "max_tokens": 1024                     # 可选参数，限制回复长度
}

try:
    # 发送 POST 请求
    response = requests.post(url, headers=headers, json=payload)
    response.raise_for_status()  # 如果请求失败，抛出 HTTPError

    # 解析并打印响应
    response_data = response.json()
    print("模型回复：")
    print(response_data)
    print(response_data.get("output", [{"content": [{"text": "无响应内容"}]}])[0]["content"][0]["text"])

except requests.exceptions.RequestException as e:
    print(f"请求时发生错误: {e}")
    if 'response' in locals():
        print(f"响应状态码: {response.status_code}")
        print(f"响应内容: {response.text}")