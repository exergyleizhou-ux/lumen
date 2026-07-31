import zipfile
import xml.etree.ElementTree as ET
import sys
import io

# Fix encoding for Windows console
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8', errors='replace')

docx_path = r"C:\Users\开心的阿木木\Desktop\咋了.docx"
try:
    with zipfile.ZipFile(docx_path, 'r') as z:
        with z.open('word/document.xml') as f:
            tree = ET.parse(f)
            root = tree.getroot()
            
            ns = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'
            
            for para in root.iter(f'{{{ns}}}p'):
                texts = []
                for t in para.iter(f'{{{ns}}}t'):
                    if t.text:
                        texts.append(t.text)
                line = ''.join(texts)
                if line.strip():
                    print(line)
except Exception as e:
    print(f"Error: {e}")
