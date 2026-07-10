#include <unistd.h>

char	*ft_strcat(char *dest, char *src);

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
	char	dest[256];
	int		i;

	if (argc < 3)
		return (1);
	i = 0;
	while (argv[1][i] && i < 255)
	{
		dest[i] = argv[1][i];
		i++;
	}
	dest[i] = '\0';
	ft_strcat(dest, argv[2]);
	write(1, dest, my_strlen(dest));
	return (0);
}
