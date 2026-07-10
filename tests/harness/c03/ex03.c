#include <stdlib.h>
#include <unistd.h>

char	*ft_strncat(char *dest, char *src, unsigned int nb);

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
	char			dest[256];
	unsigned int	nb;
	int				i;

	if (argc < 4)
		return (1);
	nb = (unsigned int)atoi(argv[3]);
	i = 0;
	while (argv[1][i] && i < 255)
	{
		dest[i] = argv[1][i];
		i++;
	}
	dest[i] = '\0';
	ft_strncat(dest, argv[2], nb);
	write(1, dest, my_strlen(dest));
	return (0);
}
