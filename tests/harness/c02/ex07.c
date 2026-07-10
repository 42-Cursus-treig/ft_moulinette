#include <unistd.h>

char	*ft_strupcase(char *str);

static int	my_strlen(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	return (i);
}

int	main(int argc, char **argv)
{
	char	buf[256];
	int		i;

	if (argc < 2)
		return (1);
	i = 0;
	while (argv[1][i] && i < 255)
	{
		buf[i] = argv[1][i];
		i++;
	}
	buf[i] = '\0';
	ft_strupcase(buf);
	write(1, buf, my_strlen(buf));
	return (0);
}
