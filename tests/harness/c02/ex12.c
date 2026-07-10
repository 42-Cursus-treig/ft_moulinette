#include <unistd.h>

void	*ft_print_memory(void *addr, unsigned int size);

static void	fail(const char *msg)
{
	int	i;

	write(1, "FAIL: ", 6);
	i = 0;
	while (msg[i])
		i++;
	write(1, msg, i);
	_exit(1);
}

static int	is_lower_hex(char c)
{
	return ((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'));
}

/* Vérifie le format d'une ligne complète de 16 octets, sans dépendre de
   l'adresse réelle : seuls son format (16 hex minuscules + ':') est
   contrôlé, le contenu hexa/texte est comparé exactement. */
static void	check_full_line(char *buf, int len)
{
	int			i;
	const char	*expected_hex = "426f 6e6a 6f75 7220 616d 6901 0203 0405";
	const char	*expected_txt = "Bonjour ami.....";

	if (len != 16 + 2 + 39 + 1 + 17)
		fail("longueur de ligne inattendue");
	i = 0;
	while (i < 16)
	{
		if (!is_lower_hex(buf[i]))
			fail("adresse: caractere hexadecimal minuscule attendu");
		i++;
	}
	if (buf[16] != ':' || buf[17] != ' ')
		fail("format attendu apres l'adresse : ': '");
	i = 0;
	while (i < 39)
	{
		if (buf[18 + i] != expected_hex[i])
			fail("colonne hexadecimale incorrecte");
		i++;
	}
	if (buf[57] != ' ')
		fail("espace attendu avant la colonne texte");
	i = 0;
	while (i < 16)
	{
		if (buf[58 + i] != expected_txt[i])
			fail("colonne texte incorrecte");
		i++;
	}
	if (buf[74] != '\n')
		fail("retour a la ligne final manquant");
}

int	main(int argc, char **argv)
{
	unsigned char	data[16];
	int				pipefd[2];
	int				saved_stdout;
	int				len;
	void			*ret;
	static char		buf[4096];

	if (argc < 2)
		return (1);
	data[0] = 'B'; data[1] = 'o'; data[2] = 'n'; data[3] = 'j';
	data[4] = 'o'; data[5] = 'u'; data[6] = 'r'; data[7] = ' ';
	data[8] = 'a'; data[9] = 'm'; data[10] = 'i';
	data[11] = 1; data[12] = 2; data[13] = 3; data[14] = 4; data[15] = 5;

	if (pipe(pipefd) == -1)
		return (1);
	saved_stdout = dup(1);
	dup2(pipefd[1], 1);
	close(pipefd[1]);

	if (argv[1][0] == '0')
		ret = ft_print_memory(data, 0);
	else
		ret = ft_print_memory(data, 16);

	dup2(saved_stdout, 1);
	close(saved_stdout);

	len = read(pipefd[0], buf, sizeof(buf) - 1);
	if (len < 0)
		len = 0;
	buf[len] = '\0';
	close(pipefd[0]);

	if (ret != (void *)data)
		fail("la valeur de retour doit etre le pointeur addr recu");
	if (argv[1][0] == '0')
	{
		if (len != 0)
			fail("size == 0 : rien ne doit etre affiche");
	}
	else
		check_full_line(buf, len);

	write(1, "OK", 2);
	return (0);
}
