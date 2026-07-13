import re

_ACCENTS = str.maketrans("üäëéíóúőàçñ", "uaeeiouoacn")
_FILLER = re.compile(r"racing circuit|race track|circuit|layout|motor|international| gp | grand prix|speedway|raceway")

def normalize(name: str) -> str:
    s = name.lower().translate(_ACCENTS)
    s = s.replace("centre", "center")
    s = re.sub(r"reverse", "rev", s)
    s = _FILLER.sub(" ", s)
    return re.sub(r"[^a-z0-9]", "", s)
