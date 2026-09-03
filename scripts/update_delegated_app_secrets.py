from pathlib import Path


PATH = Path("/etc/lingce/secrets.toml")

OLD = """[wecom.delegated_app]
suite_id = "dka3fffb6a3be8c955"
suite_secret = "BCZhmnpa1cCP1LqEd51uQbiWmZA7KPbUQhB_8MKF0Ug"
token = "aaV3S4fLYnIqI"
encoding_aes_key = "w9xSJrCkB1G9xnSvEHksC1PFHxOogy1Cgn4Li86kKnh"
callback_base_url = "https://wecom.khgl.xyz"
install_redirect_url = "https://wecom.khgl.xyz/api/v1/wecom/delegated-app/install/callback"
install_auth_type = 1
"""

NEW = """[wecom.delegated_app]
suite_id = "dka3fffb6a3be8c955"
suite_secret = "BCZhmnpa1cCP1LqEd51uQbiWmZA7KPbUQhB_8MKF0Ug"
token = "IPgXxjJ96ysqMJ61aVbcmrXPg1JoB"
encoding_aes_key = "hO9leAj8csxOs7qT1c5WUp16BrV7n9yPJVEd3lQBy7V"
callback_base_url = "https://wecom.khgl.xyz"
install_redirect_url = "https://wecom.khgl.xyz/api/v1/wecom/delegated-app/install/callback"
install_auth_type = 1

[wecom.delegated_app.enterprise_callback]
token = "aaV3S4fLYnIqI"
encoding_aes_key = "w9xSJrCkB1G9xnSvEHksC1PFHxOogy1Cgn4Li86kKnh"
"""


def main() -> None:
    text = PATH.read_text()
    if OLD not in text:
        raise SystemExit("target block not found in /etc/lingce/secrets.toml")
    backup = PATH.with_name("secrets.toml.bak-20260817-delegated-callback-split")
    backup.write_text(text)
    PATH.write_text(text.replace(OLD, NEW, 1))
    print("updated", PATH)
    print("backup", backup)


if __name__ == "__main__":
    main()
